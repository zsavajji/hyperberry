/* h264gate: can the Q10 decode H.264 in hardware from memory, frame by frame? (OpenMAX AL probe)
 * Build: docker run --rm -v "$PWD:/src" -w /src hyperberry-build bb10-clang -O2 tools/h264gate/h264gate.c -o build/h264gate -lOpenMAXAL -lscreen
 * Streams (build/): ffmpeg -f lavfi -i testsrc=size=320x240:rate=15 -t 10 -c:v libx264 -profile:v baseline -level 3.0 -bf 0 -g 30 -pix_fmt yuv420p -f h264 build/t320.264
 *                   ffmpeg -f lavfi -i testsrc=size=640x480:rate=30 -t 10 -c:v libx264 -profile:v baseline -level 3.1 -bf 0 -g 60 -pix_fmt yuv420p -f h264 build/t640.264
 * Run:   h264gate [-w W -h H -f FPS] [-a asfast] [-c cfg|inband|both] [-l q|a] [-s bq|nd] file.264
 * Probe flags: -q list video decoders | -t input is MPEG-TS | -F <mime>|null source format | -E enqueue before Realize,
 *   Realize in a thread, keep feeding | -e EOS before Realize returns | -R/-A other Realize orders | -D/-C/-x format tweaks
 * Result on the Q10 (10.3.3): see the report; every Realize with the QNX encoded format returns CONTENT_UNSUPPORTED.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <time.h>
#include <pthread.h>
#include <OMXAL/OpenMAXAL.h>
#include <OMXAL/OpenMAXAL_Android.h>
#include <OMXAL/OpenMAXAL_QNX.h>
#include <screen/screen.h>

#define N 3000
static const char *errname(XAresult r) {
	static const char *n[] = {"SUCCESS", "PRECONDITIONS_VIOLATED", "PARAMETER_INVALID", "MEMORY_FAILURE",
		"RESOURCE_ERROR", "RESOURCE_LOST", "IO_ERROR", "BUFFER_INSUFFICIENT", "CONTENT_CORRUPTED",
		"CONTENT_UNSUPPORTED", "CONTENT_NOT_FOUND", "PERMISSION_DENIED", "FEATURE_UNSUPPORTED",
		"INTERNAL_ERROR", "UNKNOWN_ERROR", "OPERATION_ABORTED", "CONTROL_LOST", "READONLY",
		"ENGINEOPTION_UNSUPPORTED", "SOURCE_SINK_INCOMPATIBLE"};
	return r < 20 ? n[r] : "?";
}
#define CK(what, e) do { XAresult r_ = (e); printf("%-34s -> 0x%x XA_RESULT_%s\n", what, r_, errname(r_)); fflush(stdout); \
	if (r_) { rc = 1; goto done; } } while (0)

static double now(void) { struct timespec t; clock_gettime(CLOCK_MONOTONIC, &t); return t.tv_sec * 1e3 + t.tv_nsec / 1e6; }
static double cpu(void) { struct timespec t; clock_gettime(CLOCK_PROCESS_CPUTIME_ID, &t); return t.tv_sec * 1e3 + t.tv_nsec / 1e6; }

static int rc_unused; static unsigned char *file, *au[N]; static size_t aulen[N]; static int nau;
static unsigned char *cfg; static size_t cfglen;
static double tin[N], tout[N]; static int nin, nout, cbdone;
static int inband = 1;

/* split Annex-B into access units; SPS/PPS also collected into cfg (with start codes) */
/* MPEG-TS: one "access unit" = the packets from one video PES start (with any PAT/PMT before it) to the next */
static void split_ts(size_t len) {
	int vpid = -1, hasv = 0; size_t start = 0;
	for (size_t o = 0; o + 188 <= len; o += 188) {
		int pid = ((file[o + 1] & 31) << 8) | file[o + 2], pusi = file[o + 1] & 0x40;
		if (vpid < 0 && pusi && pid >= 0x100 && pid != 0x1000) vpid = pid;
		if (pusi && (pid == 0 || pid == vpid) && hasv && nau < N) { au[nau] = file + start; aulen[nau++] = o - start; start = o; hasv = 0; }
		if (pusi && pid == vpid) hasv = 1;
	}
	if (nau < N) { au[nau] = file + start; aulen[nau++] = len / 188 * 188 - start; }
}
static int ts;
static void split(size_t len) {
	if (ts) { split_ts(len); return; }
	size_t i = 0, start = 0, aus = 0; int have_vcl = 0, first = 1; cfg = malloc(512);
	size_t pos[N * 4]; int np = 0;
	for (i = 0; i + 3 < len && np < N * 4 - 1; i++)
		if (file[i] == 0 && file[i + 1] == 0 && file[i + 2] == 1) { pos[np++] = i > 0 && file[i - 1] == 0 ? i - 1 : i; i += 2; }
	pos[np] = len;
	for (int k = 0; k < np; k++) {
		size_t sc = file[pos[k]] == 0 && file[pos[k] + 1] == 0 && file[pos[k] + 2] == 1 ? 3 : 4;
		int t = file[pos[k] + sc] & 31;
		if ((t == 7 || t == 8) && !nau) { memcpy(cfg + cfglen, file + pos[k], pos[k + 1] - pos[k]); cfglen += pos[k + 1] - pos[k]; }
		if (first) { start = pos[k]; first = 0; }
		else if (have_vcl && (t == 1 || t == 5 || t == 6 || t == 7 || t == 8 || t == 9)) {
			au[nau] = file + start; aulen[nau++] = pos[k] - start; start = pos[k]; have_vcl = 0; if (nau >= N) return;
		}
		if (t == 1 || t == 5) have_vcl = 1;
	}
	au[nau] = file + start; aulen[nau++] = len - start; (void)aus;
}

static int eosearly, eosdone; static volatile int realize_done; static XAresult realize_res;
static void *realize_thr(void *o) { XAObjectItf p = o; realize_res = (*p)->Realize(p, XA_BOOLEAN_FALSE); realize_done = 1; return NULL; }
static pthread_mutex_t mu = PTHREAD_MUTEX_INITIALIZER;
static XAresult abq_cb(XAAndroidBufferQueueItf c, void *ctx, void *bctx, void *d, XAuint32 sz, XAuint32 used,
		const XAAndroidBufferItem *it, XAuint32 il) { pthread_mutex_lock(&mu); cbdone++; pthread_mutex_unlock(&mu); return 0; }

static XAQNXBufferQueueSinkItf sink; static int stop, fmtprinted, ow, oh;
static void *puller(void *a) {
	while (!stop) {
		XAQNXBufferItem *b = sink ? (*sink)->Get(sink) : NULL;
		if (!b) { usleep(500); continue; }
		double t = now();
		if (nout < N) tout[nout] = t;
		if (nout < 3) printf("  out#%d data=%p size=%u used=%u items=%u\n", nout, b->pBufferData, b->dataSize, b->dataUsed, b->itemsLength);
		nout++; (*sink)->Done(sink, b);
	}
	return NULL;
}

int main(int argc, char **argv) {
	int q = 0, dom = 2, codec = -1, vx = 0, rr = 0, pre = 0, rc = 0, W = 320, H = 240, fps = 15, rt = 1; const char *cm = "both", *loc = "a", *snk = "bq", *ifs = "a", *mime = NULL, *path = NULL;
	for (int i = 1; i < argc; i++) {
		if (!strcmp(argv[i], "-h") && i + 1 >= argc) return 0;
		else if (!strcmp(argv[i], "-w")) W = atoi(argv[++i]);
		else if (!strcmp(argv[i], "-h")) H = atoi(argv[++i]);
		else if (!strcmp(argv[i], "-f")) fps = atoi(argv[++i]);
		else if (!strcmp(argv[i], "-a")) rt = 0;
		else if (!strcmp(argv[i], "-r")) rt = 1;
		else if (!strcmp(argv[i], "-c")) cm = argv[++i];
		else if (!strcmp(argv[i], "-l")) loc = argv[++i];
		else if (!strcmp(argv[i], "-e")) eosearly = 1;  /* EOS item after the last AU, before Realize returns */
		else if (!strcmp(argv[i], "-t")) ts = 1;  /* input is MPEG-TS (ffmpeg -c copy -f mpegts), use with -F video/mp2ts */
		else if (!strcmp(argv[i], "-q")) q = 1;  /* list the engine's video decoders and exit */
		else if (!strcmp(argv[i], "-D")) dom = atoi(argv[++i]);
		else if (!strcmp(argv[i], "-C")) codec = strtol(argv[++i], 0, 0);
		else if (!strcmp(argv[i], "-x")) vx = atoi(argv[++i]);  /* format tweaks: 1 bitrate+duration, 2 profile/level 0, 4 audio_info codec=AVC too */
		else if (!strcmp(argv[i], "-R")) rr = 1;
		else if (!strcmp(argv[i], "-E")) rr = 3;  /* enqueue before Realize (the ABQ interface exists unrealized) */
		else if (!strcmp(argv[i], "-A")) rr = 2;  /* Realize(async=TRUE), feed the queue, wait for the state */  /* Realize in a thread, feed the queue meanwhile */
		else if (!strcmp(argv[i], "-F")) mime = argv[++i];  /* source format: MIME type, or "null" */
		else if (!strcmp(argv[i], "-i")) ifs = argv[++i];
		else if (!strcmp(argv[i], "-s")) snk = argv[++i];
		else path = argv[i];
	}
	if (q) {
		XAObjectItf e = NULL; XAInterfaceID id = XA_IID_VIDEODECODERCAPABILITIES; XAboolean rq = XA_BOOLEAN_TRUE; XAVideoDecoderCapabilitiesItf dc;
		XAuint32 n = 0, ids[64]; int rc = 0;
		CK("xaCreateEngine(+VIDEODECODERCAPS)", xaCreateEngine(&e, 0, NULL, 1, &id, &rq));
		CK("engine Realize", (*e)->Realize(e, XA_BOOLEAN_FALSE));
		CK("GetInterface(VIDEODECODERCAPS)", (*e)->GetInterface(e, id, &dc));
		CK("GetVideoDecoders(count)", (*dc)->GetVideoDecoders(dc, &n, NULL));
		if (n > 64) n = 64;
		CK("GetVideoDecoders(ids)", (*dc)->GetVideoDecoders(dc, &n, ids));
		for (XAuint32 k = 0; k < n; k++)
			for (XAuint32 ix = 0, guard = 0; guard++ < 8;) {
				XAVideoCodecDescriptor d;
				if ((*dc)->GetVideoDecoderCapabilities(dc, ids[k], &ix, &d)) break;
				printf("decoder id=0x%x codec=0x%x max %ux%u@%u bitrate=%u profile=0x%x level=0x%x\n", ids[k], d.codecId, d.maxWidth, d.maxHeight, d.maxFrameRate, d.maxBitRate, d.profileSetting, d.levelSetting);
			}
qdone:
		if (e) (*e)->Destroy(e);
		return rc;
	}
	if (!path) { fprintf(stderr, "usage: h264gate [opts] file.264\n"); return 2; }
	FILE *f = fopen(path, "rb"); if (!f) { perror(path); return 2; }
	fseek(f, 0, SEEK_END); size_t len = ftell(f); fseek(f, 0, SEEK_SET);
	file = malloc(len); fread(file, 1, len, f); fclose(f); split(len);
	printf("%s: %zu bytes, %d access units, codecConfig %zu bytes; %dx%d@%d %s cfg=%s loc=%s sink=%s\n",
		path, len, nau, cfglen, W, H, fps, rt ? "realtime" : "asfast", cm, loc, snk);
	if (!strcmp(cm, "cfg")) { /* strip SPS/PPS from first AU */
		size_t o = 0; while (o + 4 < aulen[0] && (au[0][o + 4] & 31) != 5 && (au[0][o + 4] & 31) != 1) { o++; while (o + 4 < aulen[0] && !(au[0][o] == 0 && au[0][o + 1] == 0 && (au[0][o + 2] == 1 || (au[0][o + 2] == 0 && au[0][o + 3] == 1)))) o++; }
		au[0] += o; aulen[0] -= o;
	}
	int cfgin = strcmp(cm, "inband") != 0;

	XAObjectItf eng = NULL, pl = NULL; XAEngineItf ei; XAPlayItf play; XAAndroidBufferQueueItf abq;
	screen_context_t sctx = NULL; screen_window_t swin = NULL; pthread_t th; int th_ok = 0;
	int started = 0, sent = 0; double c0 = cpu(), w0 = now();
	CK("xaCreateEngine", xaCreateEngine(&eng, 0, NULL, 0, NULL, NULL));
	CK("engine Realize", (*eng)->Realize(eng, XA_BOOLEAN_FALSE));
	CK("engine GetInterface(ENGINE)", (*eng)->GetInterface(eng, XA_IID_ENGINE, &ei));

	XADataLocator_QNXBufferQueue lq = {XA_DATALOCATOR_QNXBUFFERQUEUE, 16};
	XADataLocator_AndroidBufferQueue la = {XA_DATALOCATOR_ANDROIDBUFFERQUEUE, 16};
	XADataFormat_QNXEncoded fmt; memset(&fmt, 0, sizeof fmt);
	fmt.formatType = XAQ_DATAFORMAT_ENCODED; fmt.domainType = dom;
	fmt.video_info = (XAVideoStreamInformation_QNX){XA_VIDEOCODEC_AVC, W, H, fps, 0, 0, XA_VIDEOPROFILE_AVC_BASELINE, XA_VIDEOLEVEL_AVC_31,
		cfgin ? cfg : NULL, cfgin ? cfglen : 0};
	if (codec >= 0) fmt.video_info.codecId = codec;
	if (vx & 1) { fmt.video_info.bitRate = 1000000; fmt.video_info.duration = 10000; }
	if (vx & 2) { fmt.video_info.profileSetting = 0; fmt.video_info.levelSetting = 0; }
	if (vx & 4) fmt.formatType = XA_DATAFORMAT_RAWIMAGE;
	XADataFormat_MIME fm = {XA_DATAFORMAT_MIME, (XAchar *)(mime ? mime : ""), ts ? XA_CONTAINERTYPE_MPEG_TS : XA_CONTAINERTYPE_UNSPECIFIED};
	XADataSource src = {*loc == 'q' ? (void *)&lq : (void *)&la, mime ? (!strcmp(mime, "null") ? NULL : (void *)&fm) : (void *)&fmt};

	XADataLocator_QNXScreenBufferQueue ls = {XAQ_DATALOCATOR_SCREENBUFFERQUEUE, NULL, NULL};
	XADataLocator_NativeDisplay ln = {XA_DATALOCATOR_NATIVEDISPLAY, NULL, NULL};
	if (!strcmp(snk, "nd")) {
		int usage = SCREEN_USAGE_NATIVE | SCREEN_USAGE_WRITE | SCREEN_USAGE_READ, sz[2] = {W, H};
		screen_create_context(&sctx, 0); screen_create_window(&swin, sctx);
		screen_set_window_property_iv(swin, SCREEN_PROPERTY_USAGE, &usage);
		screen_set_window_property_iv(swin, SCREEN_PROPERTY_BUFFER_SIZE, sz);
		screen_create_window_buffers(swin, 2);
		ln.hWindow = swin; ln.hDisplay = sctx;
	}
	XADataSink vs = {!strcmp(snk, "nd") ? (void *)&ln : (void *)&ls, NULL};
	XAInterfaceID ids[3] = {*loc == 'q' ? XA_IID_ANDROIDBUFFERQUEUESOURCE : XA_IID_ANDROIDBUFFERQUEUESOURCE, XA_IID_QNXVIDEOBUFFERQUEUESINK, XA_IID_PLAY};
	XAboolean req[3] = {XA_BOOLEAN_TRUE, XA_BOOLEAN_FALSE, XA_BOOLEAN_TRUE};
	int ni = 3;
	if (*ifs == 'n') { ids[0] = XA_IID_PLAY; ni = 1; }            /* PLAY only */
	else if (*ifs == 's') { ids[0] = XA_IID_QNXVIDEOBUFFERQUEUESINK; req[0] = XA_BOOLEAN_FALSE; ids[1] = XA_IID_PLAY; ni = 2; }
	else if (*ifs == 'p') { ids[0] = XA_IID_ANDROIDBUFFERQUEUESOURCE; req[0] = XA_BOOLEAN_FALSE; ids[1] = XA_IID_PLAY; ni = 2; }
	CK("CreateMediaPlayer", (*ei)->CreateMediaPlayer(ei, &pl, &src, NULL, NULL, &vs, NULL, NULL, ni, ids, req));
	alarm(60); /* watchdog: a Realize that waits for data must not hang the phone session */
	if (rr) {
		pthread_t rt_; XAresult ar = 0;
		if (rr == 2) { ar = (*pl)->Realize(pl, XA_BOOLEAN_TRUE); printf("%-34s -> 0x%x XA_RESULT_%s\n", "player Realize(async)", ar, errname(ar)); }
		else if (rr == 1) pthread_create(&rt_, NULL, realize_thr, pl);
		double t0 = now(); XAresult r = 1;
		while (now() - t0 < 5000 && (r = (*pl)->GetInterface(pl, XA_IID_ANDROIDBUFFERQUEUESOURCE, &abq))) usleep(2000);
		printf("%-34s -> 0x%x XA_RESULT_%s (after %.0f ms)\n", "early GetInterface(ANDROIDBQ)", r, errname(r), now() - t0);
		if (!r) {
			(*abq)->RegisterCallback(abq, abq_cb, NULL);
			for (pre = 0; pre < nau && pre < 8; pre++) {
				tin[pre] = now(); XAresult e = (*abq)->Enqueue(abq, NULL, au[pre], aulen[pre], NULL, 0);
				if (e) { printf("early Enqueue #%d -> 0x%x XA_RESULT_%s\n", pre, e, errname(e)); break; }
			}
			printf("early enqueued %d AUs\n", pre);
		}
		if (rr == 3) {  /* Realize in a thread while main keeps the queue fed */
			pthread_create(&rt_, NULL, realize_thr, pl);
			double t1 = now();
			while (!realize_done && now() - t1 < 8000) {
				int f; pthread_mutex_lock(&mu); f = pre - cbdone; pthread_mutex_unlock(&mu);
				if (pre < nau && f < 15) { tin[pre] = now(); if ((*abq)->Enqueue(abq, NULL, au[pre], aulen[pre], NULL, 0)) break; pre++; }
				else usleep(1000);
			}
			if (!realize_done && pre >= nau && eosearly) { XAAndroidBufferItem eos = {XA_ANDROID_ITEMKEY_EOS, 0}; printf("EOS enqueue -> 0x%x\n", (unsigned)(*abq)->Enqueue(abq, NULL, NULL, 0, &eos, sizeof eos)); eosdone = 1; for (double t2 = now(); !realize_done && now() - t2 < 4000;) usleep(1000); }
			printf("fed %d AUs while Realize ran (done=%d, %.0f ms)\n", pre, realize_done, now() - t1);
			if (!realize_done) { rc = 1; fflush(stdout); _exit(3); } /* wedged: leave the thread, bail out hard */
			pthread_join(rt_, NULL);
		}
		else if (rr == 2) {
			XAuint32 st = 0; double t1 = now();
			while (now() - t1 < 5000 && (*pl)->GetState(pl, &st) == 0 && st != XA_OBJECT_STATE_REALIZED && st != XA_OBJECT_STATE_UNREALIZED) usleep(5000);
			printf("player state after async realize: %u (1 unrealized, 2 realized, 3 suspended) after %.0f ms\n", st, now() - t1);
			realize_res = st == XA_OBJECT_STATE_REALIZED ? 0 : XA_RESULT_CONTENT_UNSUPPORTED;
		} else pthread_join(rt_, NULL);
		printf("%-34s -> 0x%x XA_RESULT_%s\n", "player Realize result", realize_res, errname(realize_res));
		if (realize_res) { rc = 1; goto done; }
		sent = nin = pre;
	} else
		CK("player Realize", (*pl)->Realize(pl, XA_BOOLEAN_FALSE));
	CK("player GetInterface(PLAY)", (*pl)->GetInterface(pl, XA_IID_PLAY, &play));
	CK("player GetInterface(ANDROIDBQ)", (*pl)->GetInterface(pl, XA_IID_ANDROIDBUFFERQUEUESOURCE, &abq));
	CK("abq RegisterCallback", (*abq)->RegisterCallback(abq, abq_cb, NULL));
	if (!strcmp(snk, "bq")) {
		CK("player GetInterface(QNXVIDEOSINK)", (*pl)->GetInterface(pl, XA_IID_QNXVIDEOBUFFERQUEUESINK, &sink));
		XADataFormat_QNXEncoded of; memset(&of, 0, sizeof of);
		XAresult r = (*sink)->GetFormat(sink, &of);
		printf("sink GetFormat -> %s: type=0x%x domain=%u video codec=0x%x %ux%u fps=%u\n", errname(r), of.formatType, of.domainType,
			of.video_info.codecId, of.video_info.width, of.video_info.height, of.video_info.frameRate);
		pthread_create(&th, NULL, puller, NULL); th_ok = 1;
	}
	CK("SetPlayState(PAUSED)", (*play)->SetPlayState(play, XA_PLAYSTATE_PAUSED));
	double base = now(); int infl = 0;
	printf("setup wall %.0f ms, cpu %.0f ms\n", base - w0, cpu() - c0);
	double c1 = cpu(), w1 = now();
	for (int i = pre; i < nau; i++) {
		if (rt) { double due = base + i * 1000.0 / fps; while (now() < due) usleep(500); }
		for (;;) { pthread_mutex_lock(&mu); infl = sent - cbdone; pthread_mutex_unlock(&mu); if (infl < 15) break; usleep(500); }
		struct { XAAndroidBufferItem h; long long ts; } item; item.h.itemKey = XAQ_ITEMKEY_TIMESTAMP; item.h.itemSize = 8;
		item.ts = (long long)i * 1000000 / fps;
		tin[i] = now();
		XAresult r = (*abq)->Enqueue(abq, NULL, au[i], aulen[i], &item.h, sizeof item);
		if (r) { printf("Enqueue #%d -> 0x%x XA_RESULT_%s\n", i, r, errname(r)); rc = 1; goto done; }
		if (i == 0) printf("Enqueue #0 -> SUCCESS (%zu bytes)\n", aulen[0]);
		sent++; nin++;
		if (!started && (sent >= 4 || i == nau - 1)) { CK("SetPlayState(PLAYING)", (*play)->SetPlayState(play, XA_PLAYSTATE_PLAYING)); started = 1; }
	}
	if (!eosdone) { XAAndroidBufferItem eos = {XA_ANDROID_ITEMKEY_EOS, 0}; (*abq)->Enqueue(abq, NULL, NULL, 0, &eos, sizeof eos); }
	{ int last = -1; double lt = now(); while (now() - lt < 2000 && nout < nin) { if (nout != last) { last = nout; lt = now(); } usleep(2000); } }
	{
		double w = now() - w1, c = cpu() - c1, sum = 0, mx = 0, mn = 1e9; int k = nout < nin ? nout : nin;
		for (int i = 0; i < k && i < N; i++) { double d = tout[i] - tin[i]; sum += d; if (d > mx) mx = d; if (d < mn) mn = d; }
		printf("RESULT frames in %d out %d | first-frame latency %.1f ms | latency mean %.1f min %.1f max %.1f ms (FIFO-matched)\n",
			nin, nout, nout ? tout[0] - tin[0] : -1.0, k ? sum / k : 0, k ? mn : 0, mx);
		printf("RESULT wall %.0f ms, own CPU %.0f ms (%.1f%% of wall)\n", w, c, 100 * c / w);
	}
done:
	stop = 1; if (th_ok) pthread_join(th, NULL);
	if (pl) { if (started) (*play)->SetPlayState(play, XA_PLAYSTATE_STOPPED); (*pl)->Destroy(pl); }
	if (eng) (*eng)->Destroy(eng);
	if (swin) screen_destroy_window(swin);
	if (sctx) screen_destroy_context(sctx);
	return rc;
}

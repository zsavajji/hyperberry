/* h264uri: H.264 in Matroska/MP4 through OpenMAX AL from a URI (file or local HTTP), decoded frames pulled from the
 * QNX video buffer-queue sink. Finished file, growing file written live, or a 127.0.0.1 HTTP stream.
 * Build: docker run --rm -v "$PWD:/src" -w /src hyperberry-build bb10-clang -w -O2 tools/h264gate/h264uri.c -o build/h264uri -lOpenMAXAL -lscreen -lsocket
 * Streams (build/): ffmpeg -f lavfi -i testsrc=size=320x240:rate=15 -t 20 -c:v libx264 -profile:v baseline -level 3.0 -bf 0 -g 30 -pix_fmt yuv420p -f matroska build/m320.mkv
 *                   ffmpeg -f lavfi -i testsrc=size=640x480:rate=30 -t 20 -c:v libx264 -profile:v baseline -level 3.1 -bf 0 -g 60 -pix_fmt yuv420p -f matroska build/m640.mkv
 *   live (streamable, unknown-size clusters, small clusters): same with  -cluster_time_limit 50 -cluster_size_limit 20000 -f matroska pipe:1 > build/s320.mkv
 * Run: h264uri [-m none|mkv|qmkv|qmkv0|mp4] [-T secs] [-P] [-S] file            (finished file)
 *      h264uri -L [-H] [-p startframe] [-m ..] [-w workfile] stream.mkv          (live: write growing file / serve HTTP)
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <time.h>
#include <fcntl.h>
#include <sys/stat.h>
#include <signal.h>
#include <errno.h>
#include <pthread.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <OMXAL/OpenMAXAL.h>
#include <OMXAL/OpenMAXAL_Android.h>
#include <OMXAL/OpenMAXAL_QNX.h>
#include <screen/screen.h>

#define N 4000
static const char *en(unsigned r) {
	static const char *n[] = {"SUCCESS", "PRECONDITIONS_VIOLATED", "PARAMETER_INVALID", "MEMORY_FAILURE", "RESOURCE_ERROR", "RESOURCE_LOST",
		"IO_ERROR", "BUFFER_INSUFFICIENT", "CONTENT_CORRUPTED", "CONTENT_UNSUPPORTED", "CONTENT_NOT_FOUND", "PERMISSION_DENIED",
		"FEATURE_UNSUPPORTED", "INTERNAL_ERROR", "UNKNOWN_ERROR", "OPERATION_ABORTED", "CONTROL_LOST", "READONLY",
		"ENGINEOPTION_UNSUPPORTED", "SOURCE_SINK_INCOMPATIBLE"};
	return r < 20 ? n[r] : "?";
}
static double now(void) { struct timespec t; clock_gettime(CLOCK_MONOTONIC, &t); return t.tv_sec * 1e3 + t.tv_nsec / 1e6; }
static double cpu(void) { struct timespec t; clock_gettime(CLOCK_PROCESS_CPUTIME_ID, &t); return t.tv_sec * 1e3 + t.tv_nsec / 1e6; }
static double T0;
#define LOG(...) do { printf("[%7.0f] ", now() - T0); printf(__VA_ARGS__); printf("\n"); fflush(stdout); } while (0)

/* ---- Matroska: header length and per-frame (end offset, time ms) ---- */
static unsigned char *buf; static size_t blen, hdrlen; static size_t fend[N]; static long ftime[N]; static int nfr;
static int vint(size_t o, long long *v, int keep) { /* returns length; v = value (marker kept for IDs); -1 unknown size */
	unsigned char c = buf[o]; int l = 1; while (l <= 8 && !(c & (0x80 >> (l - 1)))) l++;
	if (l > 8 || o + l > blen) return 0;
	long long x = keep ? c : c & ((0x80 >> (l - 1)) - 1); int ones = (c & ((0x80 >> (l - 1)) - 1)) == ((0x80 >> (l - 1)) - 1);
	for (int i = 1; i < l; i++) { x = (x << 8) | buf[o + i]; if (buf[o + i] != 0xff) ones = 0; }
	*v = (!keep && ones) ? -1 : x; return l;
}
static void parse_mkv(void) {
	size_t o = 0; long long id, sz; int l;
	l = vint(o, &id, 1); o += l; l = vint(o, &sz, 0); o += l + sz;                       /* EBML header */
	l = vint(o, &id, 1); o += l; l = vint(o, &sz, 0); o += l;                            /* Segment (size ignored) */
	long tc = 0; hdrlen = 0;
	while (o + 2 < blen) {
		size_t st = o; if (!(l = vint(o, &id, 1))) break; o += l; if (!(l = vint(o, &sz, 0))) break; o += l;
		if (id == 0x1F43B675) { if (!hdrlen) hdrlen = st; continue; }                       /* Cluster: descend */
		if (id == 0xE7 && hdrlen) { tc = 0; for (long long i = 0; i < sz; i++) tc = tc << 8 | buf[o + i]; o += sz; continue; }
		if (sz < 0) break;
		if (id == 0xA0 && hdrlen) { size_t e = o + sz, p = o; long long i2, s2; int l2;   /* BlockGroup: find Block A1 */
			while (p < e) { l2 = vint(p, &i2, 1); p += l2; l2 = vint(p, &s2, 0); p += l2;
				if (i2 == 0xA1 && nfr < N) { long long tr; int lt = vint(p, &tr, 0); fend[nfr] = e; ftime[nfr++] = tc + (short)(buf[p + lt] << 8 | buf[p + lt + 1]); }
				p += s2; } o = e; continue; }
		if (id == 0xA3 && hdrlen && nfr < N) { long long tr; int lt = vint(o, &tr, 0); fend[nfr] = o + sz; ftime[nfr++] = tc + (short)(buf[o + lt] << 8 | buf[o + lt + 1]); }
		o += sz;
		if (hdrlen && id == 0x1C53BB6B) break;                                  /* Cues/Tags after the clusters */
	}
	if (nfr) fend[nfr - 1] = blen;
}

/* ---- writer: appends the stream paced by frame time; file or HTTP ---- */
static XAQNXBufferQueueSinkItf sink; static int pollus = 500; static volatile int nout; static double tout[4000]; static int padn, hv, fifo; static volatile size_t avail; static volatile int wfr, wdone, httpmode; static double tw[N]; static int wfd = -1; static const char *wpath = "/root/hyperberry/live.mkv";
static void *writer(void *a) {
	size_t done = 0; long t0 = ftime[0];
	if (!httpmode) wfd = fifo ? open(wpath, O_WRONLY) : open(wpath, O_WRONLY | O_CREAT | O_TRUNC, 0644);   /* a FIFO open blocks until the player opens it */
	if (fifo) LOG("player opened the FIFO");
	double base = now();
	for (int i = 0; i < nfr; i++) {
		size_t to = fend[i] > blen ? blen : fend[i]; if (i == 0) to = fend[0];
		double due = base + (ftime[i] - t0); while (now() < due) usleep(500);
		if (!httpmode && write(wfd, buf + done, to - done) < 0) break;
		done = to; tw[i] = now(); avail = done; wfr = i + 1;
	}
	wdone = 1; return NULL;
}
static void *sender(void *a) {
	int c = (int)(long)a; char rq[2048]; ssize_t n = recv(c, rq, sizeof rq - 1, 0); if (n <= 0) { close(c); return NULL; } rq[n] = 0;
	LOG("http request: %s", rq); char *p = strstr(rq, "Range: bytes="); size_t from = p ? strtoull(p + 13, 0, 10) : 0;
	char h[300]; int hl;   /* hv: 0 plain 200 | 1 chunked | 2 206 with a huge fake length | 3 200 with a huge fake length */
	if (hv == 1) hl = sprintf(h, "HTTP/1.1 200 OK\r\nContent-Type: video/x-matroska\r\nTransfer-Encoding: chunked\r\n\r\n");
	else if (hv == 2) hl = sprintf(h, "HTTP/1.1 206 Partial Content\r\nContent-Type: video/x-matroska\r\nAccept-Ranges: bytes\r\nContent-Range: bytes %zu-1073741822/1073741823\r\nContent-Length: %zu\r\n\r\n", from, 1073741823 - from);
	else if (hv == 3) hl = sprintf(h, "HTTP/1.1 200 OK\r\nContent-Type: video/x-matroska\r\nContent-Length: 1073741823\r\n\r\n");
	else hl = sprintf(h, "HTTP/1.1 200 OK\r\nContent-Type: video/x-matroska\r\nConnection: close\r\n\r\n");
	send(c, h, hl, 0);
	if (strncmp(rq, "HEAD", 4) == 0) { close(c); return NULL; }
	size_t sent = from; for (;;) { size_t a2 = avail; if (sent < a2) { ssize_t s; if (hv == 1) { char ch[20]; int cl = sprintf(ch, "%zx\r\n", a2 - sent); send(c, ch, cl, 0); s = send(c, buf + sent, a2 - sent, 0); send(c, "\r\n", 2, 0); } else s = send(c, buf + sent, a2 - sent, 0); if (s <= 0) break; sent += s; } else if (wdone) break; else usleep(500); }
	close(c); return NULL;
}
static void *acceptor(void *a) {
	int s = (int)(long)a; for (;;) { int c = accept(s, 0, 0); if (c < 0) break; pthread_t t; pthread_create(&t, 0, sender, (void *)(long)c); pthread_detach(t); } return NULL;
}

/* ---- player side ---- */
static volatile int stop, headatend; static double tfirst, tplay; static volatile long long lastts; static volatile double tlast; static int sizeprobe;
static size_t out_bytes_at_first; static int out_frames_at_first; static long out_ms_at_first;
static void *puller(void *a) {
	while (!stop) {
		XAQNXBufferItem *b = (*sink)->Get(sink);
		if (!b) { usleep(pollus); continue; }
		double t = now(); tlast = t; if (nout < N) tout[nout] = t;
		if (nout == 0) { tfirst = t; out_bytes_at_first = avail; out_frames_at_first = wfr; out_ms_at_first = wfr ? ftime[wfr - 1] - ftime[0] : 0; }
		if (nout < 3) { const unsigned *w = (const unsigned *)b->pItems; LOG("  pItems=%p len=%lu words %08x %08x %08x %08x", b->pItems, (unsigned long)b->itemsLength, w ? w[0] : 0, w ? w[1] : 0, w ? w[2] : 0, w ? w[3] : 0); }
		{ const unsigned char *q = (const unsigned char *)b->pItems;   /* itemsLength counts items, each 8-byte header + data */
		  for (unsigned k = 0; q && k < b->itemsLength; k++) { const XAAndroidBufferItem *it = (const XAAndroidBufferItem *)q;
			if (it->itemKey == XAQ_ITEMKEY_TIMESTAMP && it->itemSize >= 8) { long long ts; memcpy(&ts, it->itemData, 8); lastts = ts; if (nout < 3) LOG("  item timestamp %lld us", ts); }
			else if (nout < 3) LOG("  item key 0x%x size %u", (unsigned)it->itemKey, (unsigned)it->itemSize);
			q += 8 + ((it->itemSize + 3) & ~3u); } }
		if (nout < 3) {
			LOG("frame#%d data=%p size=%lu used=%lu items=%lu", nout, b->pBufferData, (unsigned long)b->dataSize, (unsigned long)b->dataUsed, (unsigned long)b->itemsLength);
			if (sizeprobe && nout == 0) { int sz[2] = {0, 0}, fm = 0; screen_buffer_t sb = (screen_buffer_t)b->pBufferData;
				int r1 = screen_get_buffer_property_iv(sb, SCREEN_PROPERTY_BUFFER_SIZE, sz), r2 = screen_get_buffer_property_iv(sb, SCREEN_PROPERTY_FORMAT, &fm);
				LOG("screen buffer: size %dx%d (r=%d) format %d (r=%d)", sz[0], sz[1], r1, fm, r2); }
		}
		nout++; (*sink)->Done(sink, b);
	}
	return NULL;
}
static void playcb(XAPlayItf c, void *x, XAuint32 ev) { LOG("play event 0x%x%s", (unsigned)ev, ev == XA_PLAYEVENT_HEADATEND ? " HEADATEND" : ""); if (ev == XA_PLAYEVENT_HEADATEND) headatend = 1; }
static void prefcb(XAPrefetchStatusItf c, void *x, XAuint32 ev) { XAuint32 st = 0; XApermille fl = 0; (*c)->GetPrefetchStatus(c, &st); (*c)->GetFillLevel(c, &fl); LOG("prefetch event 0x%x status %u fill %u permille", (unsigned)ev, (unsigned)st, (unsigned)fl); }
static int wdsecs = 40, stallms = 4000;
static void *watchdog(void *a) { sleep(wdsecs); printf("WATCHDOG: %d s, _exit\n", wdsecs); fflush(stdout); _exit(9); return NULL; }
static void *hogs(void *a) { system("hogs -n -s 2 -i 3 -% 3c > /dev/shmem/h264uri.hogs 2>&1"); return NULL; }


/* ---- chunk mode: one small finished file per GOP, a fresh player per file ---- */
static int chunkrun(XAEngineItf ei, int per, const char *mode) {
	int out = 0; double tcpu0 = cpu(); XADataFormat_MIME fm = {XA_DATAFORMAT_MIME, (XAchar *)"video/x-matroska", XA_CONTAINERTYPE_UNSPECIFIED};
	for (int a = 0; a + per <= nfr; a += per) {
		int b = a + per - 1; while (wfr <= b && !wdone) usleep(500);
		char fn[100], uri[140]; sprintf(fn, "/root/hyperberry/chunk%d.mkv", (a / per) & 1); sprintf(uri, "file://%s", fn);
		size_t from = a ? fend[a - 1] : 0, to = fend[b]; double ta = now();
		int fd = open(fn, O_WRONLY | O_CREAT | O_TRUNC, 0644); if (a) write(fd, buf, hdrlen); write(fd, buf + from, to - from);
		if (padn) { unsigned char vh[4] = {0xEC, 0x20 | (padn >> 16), (padn >> 8) & 255, padn & 255}; static unsigned char z[1 << 17]; write(fd, vh, 4); write(fd, z, padn); }   /* EBML Void filler after the data */
		close(fd);
		XADataLocator_URI lu = {XA_DATALOCATOR_URI, (XAchar *)uri}; XADataSource src = {&lu, strcmp(mode, "none") ? (void *)&fm : NULL};
		XADataLocator_QNXScreenBufferQueue ls = {XAQ_DATALOCATOR_SCREENBUFFERQUEUE, NULL, NULL}; XADataSink vs = {&ls, NULL};
		XAInterfaceID ids[2] = {XA_IID_PLAY, XA_IID_QNXVIDEOBUFFERQUEUESINK}; XAboolean req[2] = {1, 1}; XAObjectItf pl; XAPlayItf play;
		XAresult r = (*ei)->CreateMediaPlayer(ei, &pl, &src, NULL, NULL, &vs, NULL, NULL, 2, ids, req); if (!r) r = (*pl)->Realize(pl, XA_BOOLEAN_FALSE);
		double trl = now(); if (r) { LOG("chunk %d: create/realize failed %s", a / per, en(r)); return 1; }
		(*pl)->GetInterface(pl, XA_IID_PLAY, &play); (*pl)->GetInterface(pl, XA_IID_QNXVIDEOBUFFERQUEUESINK, &sink); (*play)->SetPlayState(play, XA_PLAYSTATE_PLAYING);
		int got = 0; double lt = now(), tf = 0;
		while (got < per && now() - lt < 1500) { XAQNXBufferItem *bi = (*sink)->Get(sink); if (!bi) { usleep(pollus); continue; }
			if (!got) tf = now(); tout[out + got] = now(); got++; lt = now(); (*sink)->Done(sink, bi); }
		for (int i = 0; i < got; i++) tout[out + i] = tout[out + i];
		LOG("chunk %d (frames %d-%d, %zu bytes): written %.0f ms after its last frame arrived, realized in %.0f ms, first frame +%.0f ms, %d/%d frames", a / per, a, b, to - from + (a ? hdrlen : 0), ta - tw[b], trl - ta, tf ? tf - trl : -1.0, got, per);
		out += got; (*pl)->Destroy(pl);
	}
	nout = out; double sum = 0, mx = 0; for (int i = 0; i < out && i < nfr; i++) { double d = tout[i] - tw[i]; sum += d; if (d > mx) mx = d; }
	LOG("RESULT chunks of %d frames: frames in %d out %d | latency write->decoded mean %.0f max %.0f ms | own CPU %.0f ms over %.0f ms", per, (int)wfr, out, out ? sum / out : 0, mx, cpu() - tcpu0, now() - T0);
	return 0;
}

#define CK(w, e) do { XAresult r_ = (e); LOG("%-28s -> 0x%x %s", w, (unsigned)r_, en(r_)); if (r_) { rc = 1; goto done; } } while (0)
int main(int argc, char **argv) {
	int chunkn = 0, kick = 0, live = 0, pstart = 0, rc = 0, usepf = 0; const char *mode = "none", *path = NULL;
	for (int i = 1; i < argc; i++) {
		if (!strcmp(argv[i], "-h") && i + 1 >= argc) return 0;
		else if (!strcmp(argv[i], "-m")) mode = argv[++i];
		else if (!strcmp(argv[i], "-T")) wdsecs = atoi(argv[++i]);
		else if (!strcmp(argv[i], "-L")) live = 1;
		else if (!strcmp(argv[i], "-H")) httpmode = 1;
		else if (!strcmp(argv[i], "-V")) hv = atoi(argv[++i]);  /* HTTP reply variant */
		else if (!strcmp(argv[i], "-p")) pstart = atoi(argv[++i]);
		else if (!strcmp(argv[i], "-w")) wpath = argv[++i];
		else if (!strcmp(argv[i], "-P")) usepf = 1;
		else if (!strcmp(argv[i], "-F")) { fifo = 1; wpath = "/root/hyperberry/h264uri.fifo"; }  /* live through a named pipe */
		else if (!strcmp(argv[i], "-G")) chunkn = atoi(argv[++i]);  /* chunk mode: frames per file (needs an IDR at each chunk start) */
		else if (!strcmp(argv[i], "-z")) padn = atoi(argv[++i]);  /* chunk mode: EBML Void padding bytes after the data */
		else if (!strcmp(argv[i], "-K")) kick = 1;  /* re-seek to the last frame when playback stalls at the end of the written data */
		else if (!strcmp(argv[i], "-s")) stallms = atoi(argv[++i]);  /* give up after this many ms without a new frame */
		else if (!strcmp(argv[i], "-S")) sizeprobe = 1;
		else if (!strcmp(argv[i], "-i")) pollus = atoi(argv[++i]);  /* sink poll interval, us */
		else path = argv[i];
	}
	if (!path) { fprintf(stderr, "usage: h264uri [opts] file\n"); return 2; }
	signal(SIGPIPE, SIG_IGN); pthread_t th, wt, ht, at; pthread_create(&wt, 0, watchdog, 0); T0 = now();
	FILE *f = fopen(path, "rb"); if (!f) { perror(path); return 2; }
	fseek(f, 0, SEEK_END); blen = ftell(f); fseek(f, 0, SEEK_SET); buf = malloc(blen); fread(buf, 1, blen, f); fclose(f);
	char uri[300]; XAObjectItf eng = NULL, pl = NULL; XAEngineItf ei; XAPlayItf play; XAPrefetchStatusItf pf = NULL; int th_ok = 0, wr_ok = 0;
	if (live) {
		parse_mkv(); LOG("%s: %zu bytes, header %zu bytes, %d frames, %.1f s", path, blen, hdrlen, nfr, nfr ? (ftime[nfr - 1] - ftime[0]) / 1000.0 : 0);
		if (!nfr) { LOG("no frames parsed"); return 2; }
		if (httpmode) { int s = socket(AF_INET, SOCK_STREAM, 0), one = 1; struct sockaddr_in sa = {0}; setsockopt(s, SOL_SOCKET, SO_REUSEADDR, &one, sizeof one);
			sa.sin_family = AF_INET; sa.sin_addr.s_addr = inet_addr("127.0.0.1"); sa.sin_port = htons(18123);
			if (bind(s, (struct sockaddr *)&sa, sizeof sa) || listen(s, 4)) { LOG("bind/listen failed"); return 2; }
			pthread_create(&at, 0, acceptor, (void *)(long)s); strcpy(uri, "http://127.0.0.1:18123/live.mkv"); }
		else { sprintf(uri, "file://%s", wpath); if (fifo) { unlink(wpath); int mr = mkfifo(wpath, 0644); LOG("mkfifo %s -> %d errno %d", wpath, mr, mr ? errno : 0); } }
		pthread_create(&ht, 0, writer, 0); wr_ok = 1;
		while (!fifo && wfr <= pstart && !wdone) usleep(500);   /* header + frames 0..pstart written */
		LOG("player starts with %zu bytes, %d frames (%ld ms of video) written", (size_t)avail, (int)wfr, wfr ? ftime[wfr - 1] - ftime[0] : 0L);
	} else sprintf(uri, "file://%s", path);
	LOG("uri %s mode %s", uri, mode);

	CK("xaCreateEngine", xaCreateEngine(&eng, 0, NULL, 0, NULL, NULL));
	CK("engine Realize", (*eng)->Realize(eng, XA_BOOLEAN_FALSE));
	CK("engine GetInterface", (*eng)->GetInterface(eng, XA_IID_ENGINE, &ei));
	XADataLocator_URI lu = {XA_DATALOCATOR_URI, (XAchar *)uri};
	XADataFormat_MIME fm = {XA_DATAFORMAT_MIME, (XAchar *)"video/x-matroska", XA_CONTAINERTYPE_UNSPECIFIED};
	if (!strcmp(mode, "qmkv")) fm.containerType = XA_CONTAINERTYPE_QNX_MKV;
	else if (!strcmp(mode, "qmkv0")) { fm.pMimeType = NULL; fm.containerType = XA_CONTAINERTYPE_QNX_MKV; }
	else if (!strcmp(mode, "mp4")) { fm.pMimeType = (XAchar *)"video/mp4"; fm.containerType = XA_CONTAINERTYPE_MP4; }
	XADataSource src = {&lu, strcmp(mode, "none") ? (void *)&fm : NULL};
	XADataLocator_QNXScreenBufferQueue ls = {XAQ_DATALOCATOR_SCREENBUFFERQUEUE, NULL, NULL};
	XADataSink vs = {&ls, NULL};
	XAInterfaceID ids[4] = {XA_IID_PLAY, XA_IID_QNXVIDEOBUFFERQUEUESINK, XA_IID_PREFETCHSTATUS, XA_IID_SEEK}; XAboolean req[4] = {1, 1, 0, 0}; XASeekItf seek = NULL;
	if (chunkn) { int r = chunkrun(ei, chunkn, mode); rc = r; goto done; }
	double tc = now();
	CK("CreateMediaPlayer", (*ei)->CreateMediaPlayer(ei, &pl, &src, NULL, NULL, &vs, NULL, NULL, 4, ids, req));
	double tr = now();
	{ XAresult r = (*pl)->Realize(pl, XA_BOOLEAN_FALSE); LOG("player Realize -> 0x%x %s in %.0f ms; written so far: %zu bytes, %d frames (%ld ms)", (unsigned)r, en(r), now() - tr,
		(size_t)avail, (int)wfr, wfr ? ftime[wfr - 1] - ftime[0] : 0L); if (r) { rc = 1; goto done; } }
	CK("GetInterface(PLAY)", (*pl)->GetInterface(pl, XA_IID_PLAY, &play));
	CK("GetInterface(QNXVIDEOSINK)", (*pl)->GetInterface(pl, XA_IID_QNXVIDEOBUFFERQUEUESINK, &sink));
	(*play)->RegisterCallback(play, playcb, NULL); (*play)->SetCallbackEventsMask(play, XA_PLAYEVENT_HEADATEND);
	if ((*pl)->GetInterface(pl, XA_IID_PREFETCHSTATUS, &pf) == 0 && pf) { LOG("prefetch interface present"); if (usepf) { (*pf)->RegisterCallback(pf, prefcb, NULL);
		(*pf)->SetCallbackEventsMask(pf, XA_PREFETCHEVENT_STATUSCHANGE | XA_PREFETCHEVENT_FILLLEVELCHANGE); (*pf)->SetFillUpdatePeriod(pf, 100); } }
	{ XADataFormat_QNXEncoded of; memset(&of, 0, sizeof of); XAresult r = (*sink)->GetFormat(sink, &of);
	  LOG("sink GetFormat -> %s type=0x%x domain=%u codec=0x%x %ux%u fps=%u", en(r), (unsigned)of.formatType, (unsigned)of.domainType, (unsigned)of.video_info.codecId, (unsigned)of.video_info.width, (unsigned)of.video_info.height, (unsigned)of.video_info.frameRate); }
	pthread_create(&th, 0, puller, 0); th_ok = 1;
	double c0 = cpu(); tplay = now(); pthread_t hg; pthread_create(&hg, 0, hogs, 0);
	CK("SetPlayState(PLAYING)", (*play)->SetPlayState(play, XA_PLAYSTATE_PLAYING));
	if (kick) (*pl)->GetInterface(pl, XA_IID_SEEK, &seek);
	{ int last = -1; double lt = now(), lk = now(); while (now() - lt < stallms && !headatend) { if (nout != last) { last = nout; lt = now(); }
		if (kick && seek && nout && now() - tlast > 300 && now() - lk > 700 && !(wdone && nout >= nfr)) { lk = now(); XAresult r1, r2, r3;
			r1 = (*play)->SetPlayState(play, XA_PLAYSTATE_PAUSED); r2 = (*seek)->SetPosition(seek, lastts / 1000, XA_SEEKMODE_FAST); r3 = (*play)->SetPlayState(play, XA_PLAYSTATE_PLAYING);
			LOG("kick: pause %s, SetPosition(%lld ms) %s, play %s (frames so far %d)", en(r1), (long long)(lastts / 1000), en(r2), en(r3), nout); }
		usleep(5000); if (live && wdone && nout >= nfr) break; } }
	{ double c = cpu() - c0, w = now() - tplay, sum = 0, mx = 0; int k = 0;
	  if (live) for (int i = 0; i < nout && i < nfr; i++) { double d = tout[i] - tw[i]; sum += d; if (d > mx) mx = d; k++; }
	  LOG("RESULT frames in %d out %d | Play->first frame %.0f ms | at first frame: %zu bytes, %d frames, %ld ms of video written",
		live ? (int)wfr : -1, (int)nout, nout ? tfirst - tplay : -1.0, out_bytes_at_first, out_frames_at_first, out_ms_at_first);
	  if (live) LOG("RESULT write->decoded latency mean %.0f max %.0f ms over %d frames", k ? sum / k : 0, mx, k);
	  LOG("RESULT own CPU %.0f ms over %.0f ms wall (%.1f%%); Realize took %.0f ms (Create %.0f ms)", c, w, 100 * c / w, tplay - tr, tr - tc);
	  if (nout > 1) LOG("RESULT output rate %.1f fps; last frame timestamp %lld ms", (nout - 1) * 1000.0 / (tout[nout - 1] - tout[0]), (long long)(lastts / 1000)); }
	pthread_join(hg, NULL);
	{ FILE *h = fopen("/dev/shmem/h264uri.hogs", "r"); char l[200]; while (h && fgets(l, sizeof l, h)) printf("hogs| %s", l); if (h) fclose(h); unlink("/dev/shmem/h264uri.hogs"); }
done:
	stop = 1; if (th_ok) pthread_join(th, NULL);
	if (pl) { (*pl)->Destroy(pl); } if (eng) (*eng)->Destroy(eng);
	if (wr_ok) { wdone = 1; } if (wfd >= 0) close(wfd);
	return rc;
}

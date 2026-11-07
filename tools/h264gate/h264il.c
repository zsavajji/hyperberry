/* h264il: drive the Q10's hardware H.264 decoder (OMX.qcom.video.decoder.avc, Qualcomm's libOmxCore.so) through OpenMAX IL
 * directly, one access unit per EmptyThisBuffer, and measure.
 * Headers (Khronos IL 1.1.2, not committed): build/omxil/OMX_*.h from
 *   https://android.googlesource.com/platform/frameworks/native/+/refs/heads/main/headers/media_plugin/media/openmax/<file>?format=TEXT  (base64)
 * Build: docker run --rm -v "$PWD:/src" -w /src hyperberry-build bb10-clang -w -O2 -Ibuild/omxil tools/h264gate/h264il.c -o build/h264il
 *   (the core is dlopen()ed on the phone, nothing to link). Streams: see h264gate.c's header (t320.264, t640.264).
 * Run: h264il [-w W -h H -f FPS] [-a asfast] [-T secs] [-x flags] file.264
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <time.h>
#include <dlfcn.h>
#include <pthread.h>
#include <OMX_Core.h>
#include <OMX_Component.h>
#include <OMX_IVCommon.h>
#include <OMX_Video.h>

#define N 3000
#define NB 40
static const char *errn(OMX_ERRORTYPE e) {
	switch ((unsigned)e) {
#define E(x) case OMX_Error##x: return "OMX_Error" #x;
	E(None) E(InsufficientResources) E(Undefined) E(InvalidComponentName) E(ComponentNotFound) E(InvalidComponent) E(BadParameter)
	E(NotImplemented) E(Underflow) E(Overflow) E(Hardware) E(InvalidState) E(StreamCorrupt) E(PortsNotCompatible) E(ResourcesLost)
	E(NoMore) E(VersionMismatch) E(NotReady) E(Timeout) E(SameState) E(ResourcesPreempted) E(PortUnresponsiveDuringAllocation)
	E(PortUnresponsiveDuringDeallocation) E(PortUnresponsiveDuringStop) E(IncorrectStateTransition) E(IncorrectStateOperation)
	E(UnsupportedSetting) E(UnsupportedIndex) E(BadPortIndex) E(PortUnpopulated) E(ComponentSuspended) E(DynamicResourcesUnavailable)
	E(MbErrorsInFrame) E(FormatNotDetected) E(ContentPipeOpenFailed) E(ContentPipeCreationFailed) E(SeperateTablesUsed) E(TunnelingUnsupported)
#undef E
	} static char b[24]; sprintf(b, "0x%x", (unsigned)e); return b;
}
static double now(void) { struct timespec t; clock_gettime(CLOCK_MONOTONIC, &t); return t.tv_sec * 1e3 + t.tv_nsec / 1e6; }
static double cpu(void) { struct timespec t; clock_gettime(CLOCK_PROCESS_CPUTIME_ID, &t); return t.tv_sec * 1e3 + t.tv_nsec / 1e6; }
static double T0;
#define LOG(...) do { printf("[%6.0f] ", now() - T0); printf(__VA_ARGS__); printf("\n"); fflush(stdout); } while (0)
#define CK(w, e) do { OMX_ERRORTYPE r_ = (e); LOG("%-34s -> %s", w, errn(r_)); if (r_) { rc = 1; goto done; } } while (0)
#define INIT(x) do { memset(&(x), 0, sizeof(x)); (x).nSize = sizeof(x); (x).nVersion.s.nVersionMajor = 1; (x).nVersion.s.nVersionMinor = 1; (x).nVersion.s.nRevision = 2; } while (0)

/* ---- Annex-B access units ---- */
static unsigned char *file, *au[N]; static size_t aulen[N], maxau, cfglen; static int nau; static unsigned char cfg[1024];
static void split(size_t len) {
	static size_t pos[N * 4]; int np = 0, have = 0, first = 1; size_t start = 0;
	for (size_t i = 0; i + 3 < len && np < N * 4 - 1; i++)
		if (!file[i] && !file[i + 1] && file[i + 2] == 1) { pos[np++] = i && !file[i - 1] ? i - 1 : i; i += 2; }
	pos[np] = len;
	for (int k = 0; k < np; k++) {
		int sc = file[pos[k] + 2] == 1 ? 3 : 4, t = file[pos[k] + sc] & 31;
		if ((t == 7 || t == 8) && !nau && cfglen + pos[k + 1] - pos[k] < sizeof cfg) { memcpy(cfg + cfglen, file + pos[k], pos[k + 1] - pos[k]); cfglen += pos[k + 1] - pos[k]; }
		if (first) { start = pos[k]; first = 0; }
		else if (have && (t == 1 || t == 5 || t == 6 || t == 7 || t == 8 || t == 9)) { au[nau] = file + start; aulen[nau] = pos[k] - start; if (aulen[nau] > maxau) maxau = aulen[nau]; nau++; start = pos[k]; have = 0; if (nau >= N) return; }
		if (t == 1 || t == 5) have = 1;
	}
	au[nau] = file + start; aulen[nau] = len - start; if (aulen[nau] > maxau) maxau = aulen[nau]; nau++;
}

/* ---- callbacks and shared state ---- */
static pthread_mutex_t mu = PTHREAD_MUTEX_INITIALIZER;
static volatile int ev_state = -1, ev_dis[2], ev_ena[2], ev_changed, ev_err, ev_eos, nout, stopfill;
static OMX_BUFFERHEADERTYPE *inh[NB], *outh[NB]; static int nin_b, nout_b, inport, outport;
static OMX_BUFFERHEADERTYPE *freein[64]; static int nfreein; static OMX_BUFFERHEADERTYPE *refill[64]; static int nrefill;
static double tin[N], tout[N], tsout[N]; static unsigned sums[N]; static int distinct; static long long lastts = -1;
static int wd = 40, vx, dumpk = -1, tick = 300; static unsigned char dumpbuf[1 << 20]; static size_t dumplen;
/* Qualcomm NV12 64x32 tiles (0x7f000004 here): plane = tiles of 64x32 bytes in the Tile2m8ka order, chroma plane after the luma one padded to 8 KB */
static size_t tilepos(size_t x, size_t y, size_t w, size_t h) { size_t f = x + (y & ~1) * w; if (y & 1) f += (x & ~3) + 2; else if ((h & 1) == 0 || y != h - 1) f += (x + 2) & ~3; return f; }
static void detile(const unsigned char *src, int W, int H, unsigned char *dst) {   /* linear NV12, stride W */
	size_t tw = (W + 63) / 64, twa = (tw + 1) & ~1, th = (H + 31) / 32, cth = (H / 2 + 31) / 32, lsz = (twa * th * 2048 + 8191) & ~8191u;
	for (int p = 0; p < 2; p++) { size_t rows = p ? H / 2 : H, tr = p ? cth : th; const unsigned char *pl = src + (p ? lsz : 0); unsigned char *d = dst + (p ? (size_t)W * H : 0);
		for (size_t ty = 0; ty < tr; ty++) for (size_t tx = 0; tx < tw; tx++) { const unsigned char *t = pl + tilepos(tx, ty, twa, tr) * 2048; size_t n = W - tx * 64 < 64 ? W - tx * 64 : 64;
			for (size_t r = 0; r < 32 && ty * 32 + r < rows; r++) memcpy(d + (ty * 32 + r) * W + tx * 64, t + r * 64, n); } }
}
static volatile unsigned guard; static int dodetile; static double detile_ms; static int detile_n; static int curW, curH; static unsigned char linear[1 << 20];
static OMX_ERRORTYPE evh(OMX_HANDLETYPE h, OMX_PTR app, OMX_EVENTTYPE ev, OMX_U32 d1, OMX_U32 d2, OMX_PTR ed) {
	LOG("  event %u data1=%u data2=%u", (unsigned)ev, (unsigned)d1, (unsigned)d2);
	if (ev == OMX_EventCmdComplete) {
		if (d1 == OMX_CommandStateSet) ev_state = d2;
		else if (d1 == OMX_CommandPortDisable && d2 < 2 + 0xffff) ev_dis[d2 == (OMX_U32)outport] = 1;
		else if (d1 == OMX_CommandPortEnable) ev_ena[d2 == (OMX_U32)outport] = 1;
	} else if (ev == OMX_EventPortSettingsChanged) ev_changed = 1;
	else if (ev == OMX_EventError) ev_err = d1;
	else if (ev == OMX_EventBufferFlag) ev_eos = 1;
	return OMX_ErrorNone;
}
static OMX_ERRORTYPE ebd(OMX_HANDLETYPE h, OMX_PTR app, OMX_BUFFERHEADERTYPE *b) { pthread_mutex_lock(&mu); freein[nfreein++] = b; pthread_mutex_unlock(&mu); return OMX_ErrorNone; }
static unsigned fnv(const unsigned char *p, size_t n) { unsigned x = 2166136261u; for (size_t i = 0; i < n; i++) x = (x ^ p[i]) * 16777619u; return x; }
static OMX_ERRORTYPE fbd(OMX_HANDLETYPE h, OMX_PTR app, OMX_BUFFERHEADERTYPE *b) {
	double t = now(); int k = nout;
	if (b->nFilledLen && k < N) {
		long long ts = (long long)b->nTimeStamp; tout[k] = t; tsout[k] = (double)ts;
		unsigned s = 0; size_t luma = b->nFilledLen * 2 / 3;   /* sample 64 bytes across the luma plane (tiled or not, a moving picture changes them) */
		unsigned char pv[64]; for (int i = 0; i < 64; i++) pv[i] = b->pBuffer[b->nOffset + (luma / 64) * i + 3]; s = fnv(pv, 64); sums[k] = s;
		if (k < 3 || k % 100 == 0) LOG("  frame#%d ts=%lld len=%u offset=%u flags=0x%x luma-sample-hash=%08x", k, ts, (unsigned)b->nFilledLen, (unsigned)b->nOffset, (unsigned)b->nFlags, s);
		if (dodetile && curW * curH * 3 / 2 <= (int)sizeof linear) { double t1 = now(); detile(b->pBuffer + b->nOffset, curW, curH, linear); detile_ms += now() - t1; detile_n++; guard += linear[(detile_n * 977) % (curW * curH)]; }
		if (k == dumpk && b->nFilledLen <= sizeof dumpbuf) { memcpy(dumpbuf, b->pBuffer + b->nOffset, b->nFilledLen); dumplen = b->nFilledLen; }
		nout = k + 1;
	}
	if (b->nFlags & OMX_BUFFERFLAG_EOS) ev_eos = 1;
	pthread_mutex_lock(&mu); refill[nrefill++] = b; pthread_mutex_unlock(&mu); return OMX_ErrorNone;
}
static void *watchdog(void *a) { sleep(wd); printf("WATCHDOG %d s: _exit\n", wd); fflush(stdout); _exit(9); return NULL; }
static int waitfor(volatile int *v, int val, int ms) { double t = now(); while (*v != val && now() - t < ms && !ev_err) usleep(500); return *v == val; }

int main(int argc, char **argv) {
	int W = 320, H = 240, fps = 15, rt = 1, rc = 0, i; const char *path = NULL; OMX_HANDLETYPE h = NULL; int inited = 0;
	for (i = 1; i < argc; i++) {
		if (!strcmp(argv[i], "-h") && i + 1 >= argc) return 0;
		else if (!strcmp(argv[i], "-w")) W = atoi(argv[++i]); else if (!strcmp(argv[i], "-h")) H = atoi(argv[++i]);
		else if (!strcmp(argv[i], "-f")) fps = atoi(argv[++i]); else if (!strcmp(argv[i], "-a")) rt = 0;
		else if (!strcmp(argv[i], "-T")) wd = atoi(argv[++i]); else if (!strcmp(argv[i], "-x")) vx = atoi(argv[++i]); else if (!strcmp(argv[i], "-D")) dodetile = 1;   /* de-tile every frame into linear NV12 and time it */ else if (!strcmp(argv[i], "-u")) tick = atoi(argv[++i]); /* main loop sleep, us */ else if (!strcmp(argv[i], "-d")) dumpk = atoi(argv[++i]);   /* dump output frame k raw to /root/hyperberry/frame.raw */ else path = argv[i];
	}
	if (!path) { fprintf(stderr, "usage: h264il [opts] file.264\n"); return 2; }
	pthread_t wt; pthread_create(&wt, 0, watchdog, 0); T0 = now();
	FILE *f = fopen(path, "rb"); if (!f) { perror(path); return 2; }
	fseek(f, 0, SEEK_END); size_t len = ftell(f); fseek(f, 0, SEEK_SET); file = malloc(len); fread(file, 1, len, f); fclose(f); split(len);
	LOG("%s: %zu bytes, %d access units (largest %zu), SPS+PPS %zu bytes; %dx%d@%d %s", path, len, nau, maxau, cfglen, W, H, fps, rt ? "realtime" : "as fast as it goes");

	void *core = dlopen("libOmxCore.so", RTLD_NOW); if (!core) { LOG("dlopen libOmxCore.so: %s", dlerror()); return 1; }
	OMX_ERRORTYPE (*p_init)(void) = dlsym(core, "OMX_Init"), (*p_deinit)(void) = dlsym(core, "OMX_Deinit");
	OMX_ERRORTYPE (*p_enum)(OMX_STRING, OMX_U32, OMX_U32) = dlsym(core, "OMX_ComponentNameEnum");
	OMX_ERRORTYPE (*p_roles)(OMX_STRING, OMX_U32 *, OMX_U8 **) = dlsym(core, "OMX_GetRolesOfComponent");
	OMX_ERRORTYPE (*p_get)(OMX_HANDLETYPE *, OMX_STRING, OMX_PTR, OMX_CALLBACKTYPE *) = dlsym(core, "OMX_GetHandle");
	OMX_ERRORTYPE (*p_free)(OMX_HANDLETYPE) = dlsym(core, "OMX_FreeHandle");
	if (!p_init || !p_get) { LOG("missing core symbols"); return 1; }
	CK("OMX_Init", p_init()); inited = 1;
	{ char nm[128]; for (OMX_U32 k = 0; p_enum(nm, sizeof nm, k) == OMX_ErrorNone; k++) {
		OMX_U32 nr = 0; p_roles(nm, &nr, NULL); char roles[200] = ""; OMX_U8 *rp[4]; static OMX_U8 rb[4][OMX_MAX_STRINGNAME_SIZE]; for (int q = 0; q < 4; q++) rp[q] = rb[q];
		if (nr > 4) nr = 4; if (nr && p_roles(nm, &nr, rp) == OMX_ErrorNone) for (OMX_U32 q = 0; q < nr; q++) { strcat(roles, (char *)rp[q]); strcat(roles, " "); }
		LOG("  component %-34s roles: %s", nm, roles); } }
	OMX_CALLBACKTYPE cb = {evh, ebd, fbd};
	CK("OMX_GetHandle(decoder.avc)", p_get(&h, "OMX.qcom.video.decoder.avc", NULL, &cb));
	{ char nm[128]; OMX_VERSIONTYPE cv, sv; OMX_UUIDTYPE u; if (OMX_GetComponentVersion(h, nm, &cv, &sv, &u) == OMX_ErrorNone) LOG("component %s v%u.%u.%u.%u, spec %u.%u.%u.%u", nm, cv.s.nVersionMajor, cv.s.nVersionMinor, cv.s.nRevision, cv.s.nStep, sv.s.nVersionMajor, sv.s.nVersionMinor, sv.s.nRevision, sv.s.nStep); }
	OMX_PORT_PARAM_TYPE pp; INIT(pp); CK("GetParameter(VideoInit)", OMX_GetParameter(h, OMX_IndexParamVideoInit, &pp));
	inport = pp.nStartPortNumber; outport = inport + 1; LOG("ports: %u video ports from %u", (unsigned)pp.nPorts, (unsigned)pp.nStartPortNumber);
	OMX_PARAM_PORTDEFINITIONTYPE di, dout; INIT(di); di.nPortIndex = inport; INIT(dout); dout.nPortIndex = outport;
	CK("GetParameter(in def)", OMX_GetParameter(h, OMX_IndexParamPortDefinition, &di)); CK("GetParameter(out def)", OMX_GetParameter(h, OMX_IndexParamPortDefinition, &dout));
#define PRDEF(n, d) LOG("%s port %u dir %d: domain %d count actual %u min %u size %u | video %ux%u stride %d slice %u codec %d color 0x%x fps(q16) %u", n, (unsigned)(d).nPortIndex, (int)(d).eDir, (int)(d).eDomain, (unsigned)(d).nBufferCountActual, (unsigned)(d).nBufferCountMin, (unsigned)(d).nBufferSize, (unsigned)(d).format.video.nFrameWidth, (unsigned)(d).format.video.nFrameHeight, (int)(d).format.video.nStride, (unsigned)(d).format.video.nSliceHeight, (int)(d).format.video.eCompressionFormat, (unsigned)(d).format.video.eColorFormat, (unsigned)(d).format.video.xFramerate)
	PRDEF("input ", di); PRDEF("output", dout);
	{ OMX_VIDEO_PARAM_PORTFORMATTYPE pf; for (OMX_U32 k = 0; k < 8; k++) { INIT(pf); pf.nPortIndex = outport; pf.nIndex = k; if (OMX_GetParameter(h, OMX_IndexParamVideoPortFormat, &pf)) break; LOG("  output format #%u: color 0x%x compression %d", (unsigned)k, (unsigned)pf.eColorFormat, (int)pf.eCompressionFormat); } }
	di.format.video.nFrameWidth = W; di.format.video.nFrameHeight = H; di.format.video.xFramerate = fps << 16; di.format.video.eCompressionFormat = OMX_VIDEO_CodingAVC;
	if (di.nBufferSize < maxau + 4096) di.nBufferSize = (maxau + 4096 + 4095) & ~4095u;
	CK("SetParameter(in def: AVC)", OMX_SetParameter(h, OMX_IndexParamPortDefinition, &di));
	{ OMX_VIDEO_PARAM_PORTFORMATTYPE pf; INIT(pf); pf.nPortIndex = inport; pf.eCompressionFormat = OMX_VIDEO_CodingAVC; pf.xFramerate = fps << 16; LOG("SetParameter(in VideoPortFormat) -> %s", errn(OMX_SetParameter(h, OMX_IndexParamVideoPortFormat, &pf))); }
	INIT(di); di.nPortIndex = inport; INIT(dout); dout.nPortIndex = outport; OMX_GetParameter(h, OMX_IndexParamPortDefinition, &di); OMX_GetParameter(h, OMX_IndexParamPortDefinition, &dout);
	PRDEF("input ", di); PRDEF("output", dout);

	curW = W; curH = H;
	CK("SendCommand(Idle)", OMX_SendCommand(h, OMX_CommandStateSet, OMX_StateIdle, NULL));
	for (nin_b = 0; nin_b < (int)di.nBufferCountActual && nin_b < NB; nin_b++) { OMX_ERRORTYPE e = OMX_AllocateBuffer(h, &inh[nin_b], inport, NULL, di.nBufferSize); if (e) { LOG("AllocateBuffer(in #%d) -> %s", nin_b, errn(e)); rc = 1; goto done; } freein[nfreein++] = inh[nin_b]; }
	LOG("in buffers: %d x %u bytes (header nSize %u, ours %zu)", nin_b, (unsigned)di.nBufferSize, (unsigned)inh[0]->nSize, sizeof(OMX_BUFFERHEADERTYPE));
	for (nout_b = 0; nout_b < (int)dout.nBufferCountActual && nout_b < NB; nout_b++) { OMX_ERRORTYPE e = OMX_AllocateBuffer(h, &outh[nout_b], outport, NULL, dout.nBufferSize); if (e) { LOG("AllocateBuffer(out #%d) -> %s", nout_b, errn(e)); rc = 1; goto done; } }
	LOG("out buffers: %d x %u bytes", nout_b, (unsigned)dout.nBufferSize);
	if (!waitfor(&ev_state, OMX_StateIdle, 3000)) { LOG("never reached Idle"); rc = 1; goto done; } LOG("state Idle");
	ev_state = -1; CK("SendCommand(Executing)", OMX_SendCommand(h, OMX_CommandStateSet, OMX_StateExecuting, NULL));
	if (!waitfor(&ev_state, OMX_StateExecuting, 3000)) { LOG("never reached Executing"); rc = 1; goto done; } LOG("state Executing");
	for (i = 0; i < nout_b; i++) CK("FillThisBuffer(initial)", OMX_FillThisBuffer(h, outh[i]));

	double c0 = cpu(), base = now(), tlast = now(); int sent = 0, sentcfg = (vx & 1) != 0, eossent = 0;   /* -x 1: no separate config buffer (SPS/PPS only in-band), 2: config buffer also ENDOFFRAME, 4: config buffer carries the first AU too */
	system("pidin ar > /dev/shmem/h264il.pidin 2>&1");   /* so the report can name the decoder-side processes */
	while (!ev_err) {
		OMX_BUFFERHEADERTYPE *b;
		pthread_mutex_lock(&mu); while (nrefill) { b = refill[--nrefill]; pthread_mutex_unlock(&mu); if (!stopfill && !ev_eos) { OMX_ERRORTYPE e = OMX_FillThisBuffer(h, b); if (e) LOG("FillThisBuffer(refill) -> %s", errn(e)); } pthread_mutex_lock(&mu); } pthread_mutex_unlock(&mu);
		if (ev_changed) {   /* output port settings changed: disable, free, re-read, re-allocate, enable */
			ev_changed = 0; stopfill = 1; LOG("output port settings changed - reconfiguring"); ev_dis[1] = 0; ev_ena[1] = 0;
			LOG("SendCommand(PortDisable out) -> %s", errn(OMX_SendCommand(h, OMX_CommandPortDisable, outport, NULL)));
			for (i = 0; i < nout_b; i++) OMX_FreeBuffer(h, outport, outh[i]);
			waitfor(&ev_dis[1], 1, 2000); INIT(dout); dout.nPortIndex = outport; OMX_GetParameter(h, OMX_IndexParamPortDefinition, &dout); PRDEF("output (new)", dout); curW = dout.format.video.nFrameWidth; curH = dout.format.video.nFrameHeight;
			LOG("SendCommand(PortEnable out) -> %s", errn(OMX_SendCommand(h, OMX_CommandPortEnable, outport, NULL)));
			for (nout_b = 0; nout_b < (int)dout.nBufferCountActual && nout_b < NB; nout_b++) { OMX_ERRORTYPE e = OMX_AllocateBuffer(h, &outh[nout_b], outport, NULL, dout.nBufferSize); if (e) { LOG("AllocateBuffer(out) -> %s", errn(e)); break; } }
			waitfor(&ev_ena[1], 1, 2000); nrefill = 0; stopfill = 0; for (i = 0; i < nout_b; i++) OMX_FillThisBuffer(h, outh[i]); LOG("out buffers now %d x %u", nout_b, (unsigned)dout.nBufferSize);
		}
		if (sent >= nau && nout >= sent) break;
		if (sent >= nau && now() - tlast > 2500) break;
		if (sent < nau || !sentcfg) {
			int due = !rt || now() >= base + sent * 1000.0 / fps; b = NULL;
			if (due) { pthread_mutex_lock(&mu); if (nfreein) b = freein[--nfreein]; pthread_mutex_unlock(&mu); }
			if (b) {
				if (!sentcfg) { memcpy(b->pBuffer, cfg, cfglen); b->nFilledLen = cfglen; b->nOffset = 0; b->nFlags = OMX_BUFFERFLAG_CODECCONFIG | ((vx & 2) ? OMX_BUFFERFLAG_ENDOFFRAME : 0); b->nTimeStamp = 0; sentcfg = 1;
					LOG("EmptyThisBuffer(SPS+PPS, CODECCONFIG) -> %s", errn(OMX_EmptyThisBuffer(h, b))); continue; }
				memcpy(b->pBuffer, au[sent], aulen[sent]); b->nFilledLen = aulen[sent]; b->nOffset = 0;
				b->nFlags = OMX_BUFFERFLAG_ENDOFFRAME | (sent == 0 ? OMX_BUFFERFLAG_STARTTIME : 0); b->nTimeStamp = (long long)sent * 1000000 / fps;
				tin[sent] = now(); OMX_ERRORTYPE e = OMX_EmptyThisBuffer(h, b); if (e) { LOG("EmptyThisBuffer #%d -> %s", sent, errn(e)); rc = 1; break; } sent++; tlast = now();
				if (sent == 1) system("pidin -p $(pidin ar | awk '/h264il/ {print $1; exit}') fds > /dev/shmem/h264il.fds 2>&1");
				continue; }
		}
		if (sent >= nau && !eossent) { pthread_mutex_lock(&mu); b = nfreein ? freein[--nfreein] : NULL; pthread_mutex_unlock(&mu);
			if (b) { b->nFilledLen = 0; b->nOffset = 0; b->nFlags = OMX_BUFFERFLAG_EOS; b->nTimeStamp = (long long)sent * 1000000 / fps; OMX_EmptyThisBuffer(h, b); eossent = 1; LOG("EOS sent"); } }
		if (nout > 0 && nout < N) tlast = tlast > tout[nout - 1] ? tlast : tout[nout - 1];
		usleep(tick);
	}
	{ double c = cpu() - c0, w = now() - base, sum = 0, mx = 0; int k = nout < sent ? nout : sent, uniq = 0; for (i = 0; i < k; i++) { double d = tout[i] - tin[i]; sum += d; if (d > mx) mx = d; int dup = 0; for (int j = 0; j < i; j++) if (sums[j] == sums[i]) { dup = 1; break; } if (!dup) uniq++; }
	  distinct = uniq; LOG("RESULT frames in %d out %d | first frame %.1f ms after the first EmptyThisBuffer | latency (FIFO-matched) mean %.1f max %.1f ms", sent, nout, nout ? tout[0] - tin[0] : -1.0, k ? sum / k : 0, mx);
	  LOG("RESULT distinct luma-sample hashes %d of %d frames; own CPU %.0f ms over %.0f ms wall (%.1f%%); output rate %.1f fps", distinct, k, c, w, 100 * c / w, nout > 1 ? (nout - 1) * 1000.0 / (tout[nout - 1] - tout[0]) : 0.0);
	  if (detile_n) LOG("RESULT de-tile to linear NV12: %.2f ms per frame over %d frames", detile_ms / detile_n, detile_n);
	  if (nout > 1) { double ts_span = (tsout[nout - 1] - tsout[0]) / 1000.0; LOG("RESULT output timestamps span %.0f ms (expected %.0f ms)", ts_span, (nout - 1) * 1000.0 / fps); } }
done:
	if (dumplen) { FILE *q = fopen("/root/hyperberry/frame.raw", "wb"); if (q) { fwrite(dumpbuf, 1, dumplen, q); fclose(q); LOG("dumped %zu bytes", dumplen); } }
	if (h) {
		stopfill = 1; ev_state = -1; LOG("SendCommand(Idle) -> %s", errn(OMX_SendCommand(h, OMX_CommandStateSet, OMX_StateIdle, NULL))); waitfor(&ev_state, OMX_StateIdle, 3000);
		ev_state = -1; LOG("SendCommand(Loaded) -> %s", errn(OMX_SendCommand(h, OMX_CommandStateSet, OMX_StateLoaded, NULL)));
		for (i = 0; i < nin_b; i++) OMX_FreeBuffer(h, inport, inh[i]); for (i = 0; i < nout_b; i++) OMX_FreeBuffer(h, outport, outh[i]);
		LOG("reached Loaded: %d", waitfor(&ev_state, OMX_StateLoaded, 3000)); LOG("OMX_FreeHandle -> %s", errn(p_free(h)));
	}
	if (inited) LOG("OMX_Deinit -> %s", errn(p_deinit()));
	{ FILE *q = fopen("/dev/shmem/h264il.fds", "r"); char l[220]; while (q && fgets(l, sizeof l, q)) printf("fds| %s", l); if (q) fclose(q); }
	unlink("/dev/shmem/h264il.fds"); unlink("/dev/shmem/h264il.pidin");
	return rc;
}

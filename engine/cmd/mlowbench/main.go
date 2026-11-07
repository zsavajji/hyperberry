// Command mlowbench times meowcaller's MLOW codec on one goroutine and reports its memory, to
// see whether a Q10's cores keep up with a call (gate 0.6). It is a measuring tool, not shipped.
package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/purpshell/meowcaller/mlow"
)

const (
	rate    = 16000
	frame   = 960 // 60 ms
	seconds = 60
)

// voice makes a pitch gliding 100-250 Hz with a few harmonics, switched on and off in 200-400 ms
// syllables: close enough to speech that the codec spends its bits as it would on a call.
func voice(n int) []float32 {
	rng := rand.New(rand.NewSource(1))
	out := make([]float32, n)
	var phase float64
	next, on := 0, false
	for i := range out {
		if i >= next {
			on = !on
			next = i + rate*(200+rng.Intn(200))/1000
			if !on {
				next = i + rate*(60+rng.Intn(100))/1000 // the pause is short
			}
		}
		f0 := 175 + 75*math.Sin(2*math.Pi*float64(i)/rate/7)
		phase += 2 * math.Pi * f0 / rate
		var s float64
		for h := 1; h <= 5; h++ {
			s += math.Sin(float64(h)*phase) / float64(h)
		}
		if on {
			s *= 0.3
		} else {
			s = 0
		}
		out[i] = float32(s + 0.01*rng.NormFloat64())
	}
	return out
}

func rms(x []float32) float64 {
	var sum float64
	for _, v := range x {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(len(x)))
}

func mem(label string) {
	var m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m)
	fmt.Printf("mem %-12s HeapAlloc %6.1f MB  HeapSys %6.1f MB  Sys %6.1f MB\n", label,
		float64(m.HeapAlloc)/1e6, float64(m.HeapSys)/1e6, float64(m.Sys)/1e6)
}

func main() {
	prof := flag.String("cpuprofile", "", "write the encode's CPU profile here")
	flag.Parse() // -h prints usage and exits 0, which is all push-engine.sh asks of a binary
	fmt.Printf("GOMAXPROCS %d\n", runtime.GOMAXPROCS(0))
	pcm := voice(rate * seconds)
	mem("before codec")
	enc, dec := mlow.NewMlowEncoder(), mlow.NewMlowDecoder()
	mem("after codec")

	// Encoding first, then decoding, so each is timed alone and nothing else competes for the cache.
	var frames [][]byte
	var bytes int
	sum := sha256.New() // the same hash before and after a speed patch: the stream is unchanged
	if *prof != "" {
		f, err := os.Create(*prof)
		if err != nil {
			panic(err)
		}
		pprof.StartCPUProfile(f)
	}
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	t0 := time.Now()
	for i := 0; i+frame <= len(pcm); i += frame {
		b, err := enc.Encode(pcm[i : i+frame])
		if err != nil {
			panic(err)
		}
		frames = append(frames, b)
		sum.Write(b)
		bytes += len(b)
	}
	te := time.Since(t0)
	runtime.ReadMemStats(&m1)
	if *prof != "" {
		pprof.StopCPUProfile()
	}
	// The garbage per frame is what a call's engine would churn through 16 times a second.
	fmt.Printf("encode allocated %.1f MB (%.0f KB a frame), %d GCs, %v paused\n",
		float64(m1.TotalAlloc-m0.TotalAlloc)/1e6, float64(m1.TotalAlloc-m0.TotalAlloc)/1e3/float64(len(frames)),
		m1.NumGC-m0.NumGC, time.Duration(m1.PauseTotalNs-m0.PauseTotalNs))

	var out []float32
	t0 = time.Now()
	for _, b := range frames {
		out = append(out, dec.Decode(b)...)
	}
	td := time.Since(t0)
	mem("after run")

	e, d := te.Seconds()/seconds, td.Seconds()/seconds
	fmt.Printf("encode %v  RTF %.3f\ndecode %v  RTF %.3f\nsum RTF %.3f\n", te, e, td, d, e+d)
	fmt.Printf("encoded %d bytes in %d frames = %.1f kbit/s, sha256 %x\n", bytes, len(frames),
		float64(bytes)*8/seconds/1000, sum.Sum(nil)[:8])
	fmt.Printf("RMS in %.4f  out %.4f  (%d samples out)\n", rms(pcm), rms(out), len(out))
}

package main

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math"
	mrand "math/rand"
	"net"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type cfg struct {
	host      string
	port      int
	clients   int
	duration  time.Duration
	pipeline  int
	keyspace  int
	valueSize int
	mode      string
	ratioGet  float64
	timeout   time.Duration
	keyDist   string
	zipfS     float64
	zipfV     float64
	multi     int
	prefill   bool
	prefillN  int
}

type workerResult struct {
	ops   uint64
	keys  uint64
	bytes uint64
	batch []time.Duration
	err   error
}

func main() {
	c := cfg{}
	flag.StringVar(&c.host, "host", "127.0.0.1", "")
	flag.IntVar(&c.port, "port", 16379, "")
	flag.IntVar(&c.clients, "clients", 50, "")
	flag.DurationVar(&c.duration, "duration", 20*time.Second, "")
	flag.IntVar(&c.pipeline, "pipeline", 16, "")
	flag.IntVar(&c.keyspace, "keyspace", 100000, "")
	flag.IntVar(&c.valueSize, "value_size", 256, "")
	flag.StringVar(&c.mode, "mode", "mixed", "")
	flag.Float64Var(&c.ratioGet, "ratio_get", 0.8, "")
	flag.DurationVar(&c.timeout, "timeout", 2*time.Second, "")
	flag.StringVar(&c.keyDist, "key_dist", "uniform", "")
	flag.Float64Var(&c.zipfS, "zipf_s", 1.2, "")
	flag.Float64Var(&c.zipfV, "zipf_v", 1.0, "")
	flag.IntVar(&c.multi, "multi", 16, "")
	flag.BoolVar(&c.prefill, "prefill", true, "")
	flag.IntVar(&c.prefillN, "prefill_n", 20000, "")
	flag.Parse()

	if c.clients <= 0 {
		c.clients = 1
	}
	if c.pipeline <= 0 {
		c.pipeline = 1
	}
	if c.keyspace <= 0 {
		c.keyspace = 1
	}
	if c.valueSize < 0 {
		c.valueSize = 0
	}
	switch c.mode {
	case "set", "get", "mixed", "incr", "mget", "mset":
	default:
		fmt.Fprintln(os.Stderr, "mode must be set|get|mixed|incr|mget|mset")
		os.Exit(2)
	}
	if c.ratioGet < 0 {
		c.ratioGet = 0
	}
	if c.ratioGet > 1 {
		c.ratioGet = 1
	}
	if c.multi <= 0 {
		c.multi = 1
	}
	switch c.keyDist {
	case "uniform", "zipf":
	default:
		fmt.Fprintln(os.Stderr, "key_dist must be uniform|zipf")
		os.Exit(2)
	}
	if c.zipfS <= 1 {
		c.zipfS = 1.2
	}
	if c.zipfV <= 0 {
		c.zipfV = 1.0
	}
	if c.prefillN < 0 {
		c.prefillN = 0
	}

	addr := net.JoinHostPort(c.host, strconv.Itoa(c.port))
	value := makeValue(c.valueSize)

	prefillKeys := min(c.keyspace, c.prefillN)
	if c.prefill && prefillKeys > 0 {
		switch c.mode {
		case "get", "mixed", "mget":
			if err := prefill(addr, c.timeout, prefillKeys, value); err != nil {
				fmt.Fprintln(os.Stderr, "prefill:", err)
				os.Exit(1)
			}
		}
	}

	deadline := time.Now().Add(c.duration)

	var totalOps uint64
	var totalKeys uint64
	var totalBytes uint64
	results := make([]workerResult, c.clients)
	var wg sync.WaitGroup
	wg.Add(c.clients)

	started := time.Now()
	for i := 0; i < c.clients; i++ {
		i := i
		go func() {
			defer wg.Done()
			r := runWorker(addr, c, value, deadline, int64(i))
			results[i] = r
			atomic.AddUint64(&totalOps, r.ops)
			atomic.AddUint64(&totalKeys, r.keys)
			atomic.AddUint64(&totalBytes, r.bytes)
		}()
	}

	wg.Wait()
	elapsed := time.Since(started)

	var allBatches []time.Duration
	for _, r := range results {
		if r.err != nil {
			fmt.Fprintln(os.Stderr, "worker error:", r.err)
		}
		allBatches = append(allBatches, r.batch...)
	}

	opsPerSec := float64(totalOps) / elapsed.Seconds()
	keysPerSec := float64(totalKeys) / elapsed.Seconds()
	mbPerSec := float64(totalBytes) / (1024 * 1024) / elapsed.Seconds()

	latP50, latP95, latP99 := batchLatencyStats(allBatches, c.pipeline)
	batP50, batP95, batP99 := batchStats(allBatches)

	fmt.Printf("target=%s mode=%s clients=%d pipeline=%d duration=%s keyspace=%d value_size=%d key_dist=%s ratio_get=%.2f multi=%d\n",
		addr, c.mode, c.clients, c.pipeline, c.duration, c.keyspace, c.valueSize, c.keyDist, c.ratioGet, c.multi)
	fmt.Printf("ops_total=%d ops_per_sec=%.0f mb_per_sec=%.2f\n", totalOps, opsPerSec, mbPerSec)
	if c.mode == "mget" || c.mode == "mset" {
		fmt.Printf("keys_total=%d keys_per_sec=%.0f keys_per_op=%d\n", totalKeys, keysPerSec, c.multi)
	} else {
		fmt.Printf("keys_total=%d keys_per_sec=%.0f\n", totalKeys, keysPerSec)
	}
	fmt.Printf("latency_per_op_ms p50=%.3f p95=%.3f p99=%.3f (batch-derived)\n",
		latP50.Seconds()*1000, latP95.Seconds()*1000, latP99.Seconds()*1000)
	fmt.Printf("latency_batch_ms p50=%.3f p95=%.3f p99=%.3f\n",
		batP50.Seconds()*1000, batP95.Seconds()*1000, batP99.Seconds()*1000)
	fmt.Printf("runtime_go_version=%s cpu=%d\n", runtime.Version(), runtime.NumCPU())
}

func prefill(addr string, timeout time.Duration, n int, value []byte) error {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	br := bufio.NewReaderSize(conn, 1<<20)
	bw := bufio.NewWriterSize(conn, 1<<20)
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("bench:%d", i)
		writeArrayHeader(bw, 3)
		writeBulkString(bw, "SET")
		writeBulkString(bw, key)
		writeBulkBytes(bw, value)
		if err := bw.Flush(); err != nil {
			return err
		}
		if err := readRESP(br); err != nil {
			return err
		}
	}
	return nil
}

func runWorker(addr string, c cfg, value []byte, deadline time.Time, seed int64) workerResult {
	conn, err := net.DialTimeout("tcp", addr, c.timeout)
	if err != nil {
		return workerResult{err: err}
	}
	defer conn.Close()

	br := bufio.NewReaderSize(conn, 1<<20)
	bw := bufio.NewWriterSize(conn, 1<<20)
	r := mrand.New(mrand.NewSource(seed ^ time.Now().UnixNano()))
	var zipf *mrand.Zipf
	if c.keyDist == "zipf" {
		zipf = mrand.NewZipf(r, c.zipfS, c.zipfV, uint64(c.keyspace-1))
	}

	batchDur := make([]time.Duration, 0, int(c.duration.Seconds())*10)
	var ops uint64
	var keys uint64
	var bytes uint64

	for time.Now().Before(deadline) {
		start := time.Now()
		n := c.pipeline
		for i := 0; i < n; i++ {
			var k int
			if zipf != nil {
				k = int(zipf.Uint64())
			} else {
				k = r.Intn(c.keyspace)
			}
			key := fmt.Sprintf("bench:%d", k)

			switch c.mode {
			case "set":
				writeArrayHeader(bw, 3)
				writeBulkString(bw, "SET")
				writeBulkString(bw, key)
				writeBulkBytes(bw, value)
				bytes += uint64(len(key) + len(value) + 32)
				keys++
			case "get":
				writeArrayHeader(bw, 2)
				writeBulkString(bw, "GET")
				writeBulkString(bw, key)
				bytes += uint64(len(key) + 16)
				keys++
			case "incr":
				writeArrayHeader(bw, 2)
				writeBulkString(bw, "INCR")
				writeBulkString(bw, key)
				bytes += uint64(len(key) + 16)
				keys++
			case "mget":
				writeArrayHeader(bw, 1+c.multi)
				writeBulkString(bw, "MGET")
				bytes += uint64(32)
				for j := 0; j < c.multi; j++ {
					var kk int
					if zipf != nil {
						kk = int(zipf.Uint64())
					} else {
						kk = r.Intn(c.keyspace)
					}
					kkey := fmt.Sprintf("bench:%d", kk)
					writeBulkString(bw, kkey)
					bytes += uint64(len(kkey) + 16)
					keys++
				}
			case "mset":
				writeArrayHeader(bw, 1+2*c.multi)
				writeBulkString(bw, "MSET")
				bytes += uint64(32)
				for j := 0; j < c.multi; j++ {
					var kk int
					if zipf != nil {
						kk = int(zipf.Uint64())
					} else {
						kk = r.Intn(c.keyspace)
					}
					kkey := fmt.Sprintf("bench:%d", kk)
					writeBulkString(bw, kkey)
					writeBulkBytes(bw, value)
					bytes += uint64(len(kkey) + len(value) + 32)
					keys++
				}
			default:
				if r.Float64() < c.ratioGet {
					writeArrayHeader(bw, 2)
					writeBulkString(bw, "GET")
					writeBulkString(bw, key)
					bytes += uint64(len(key) + 16)
					keys++
				} else {
					writeArrayHeader(bw, 3)
					writeBulkString(bw, "SET")
					writeBulkString(bw, key)
					writeBulkBytes(bw, value)
					bytes += uint64(len(key) + len(value) + 32)
					keys++
				}
			}
		}

		if err := bw.Flush(); err != nil {
			return workerResult{ops: ops, keys: keys, bytes: bytes, batch: batchDur, err: err}
		}
		for i := 0; i < n; i++ {
			if err := readRESP(br); err != nil {
				return workerResult{ops: ops, keys: keys, bytes: bytes, batch: batchDur, err: err}
			}
		}
		d := time.Since(start)
		batchDur = append(batchDur, d)
		ops += uint64(n)
	}

	return workerResult{ops: ops, keys: keys, bytes: bytes, batch: batchDur}
}

func batchLatencyStats(batch []time.Duration, pipeline int) (time.Duration, time.Duration, time.Duration) {
	if len(batch) == 0 || pipeline <= 0 {
		return 0, 0, 0
	}
	perOp := make([]time.Duration, 0, len(batch))
	div := float64(pipeline)
	for _, d := range batch {
		per := time.Duration(float64(d) / div)
		perOp = append(perOp, per)
	}
	sort.Slice(perOp, func(i, j int) bool { return perOp[i] < perOp[j] })
	p50 := perOp[int(math.Round(0.50*float64(len(perOp)-1)))]
	p95 := perOp[int(math.Round(0.95*float64(len(perOp)-1)))]
	p99 := perOp[int(math.Round(0.99*float64(len(perOp)-1)))]
	return p50, p95, p99
}

func batchStats(batch []time.Duration) (time.Duration, time.Duration, time.Duration) {
	if len(batch) == 0 {
		return 0, 0, 0
	}
	cp := append([]time.Duration(nil), batch...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	p50 := cp[int(math.Round(0.50*float64(len(cp)-1)))]
	p95 := cp[int(math.Round(0.95*float64(len(cp)-1)))]
	p99 := cp[int(math.Round(0.99*float64(len(cp)-1)))]
	return p50, p95, p99
}

func makeValue(n int) []byte {
	if n == 0 {
		return nil
	}
	b := make([]byte, n)
	_, _ = io.ReadFull(rand.Reader, b)
	for i := range b {
		b[i] = 'a' + (b[i] % 26)
	}
	return b
}

func writeArrayHeader(w *bufio.Writer, n int) {
	w.WriteByte('*')
	w.WriteString(strconv.Itoa(n))
	w.WriteString("\r\n")
}

func writeBulkString(w *bufio.Writer, s string) {
	writeBulkBytes(w, []byte(s))
}

func writeBulkBytes(w *bufio.Writer, b []byte) {
	w.WriteByte('$')
	w.WriteString(strconv.Itoa(len(b)))
	w.WriteString("\r\n")
	if len(b) > 0 {
		w.Write(b)
	}
	w.WriteString("\r\n")
}

func readRESP(r *bufio.Reader) error {
	b, err := r.ReadByte()
	if err != nil {
		return err
	}
	switch b {
	case '+', '-', ':':
		_, err := readLine(r)
		return err
	case '$':
		line, err := readLine(r)
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil {
			return err
		}
		if n < 0 {
			return nil
		}
		if _, err := io.CopyN(io.Discard, r, int64(n+2)); err != nil {
			return err
		}
		return nil
	case '*':
		line, err := readLine(r)
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil {
			return err
		}
		for i := 0; i < n; i++ {
			if err := readRESP(r); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("invalid resp type byte: %q", b)
	}
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return "", fmt.Errorf("invalid line")
	}
	return line[:len(line)-2], nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func init() {
	var seed int64
	_ = binary.Read(rand.Reader, binary.LittleEndian, &seed)
	mrand.Seed(seed)
}

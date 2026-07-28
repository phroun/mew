package trinkets

import "testing"

// termWorkload builds ~n bytes of realistic, escape-heavy terminal output
// (colored `ls -l`-style lines), the kind of data that stresses the parser.
func termWorkload(n int) []byte {
	line := []byte("\x1b[0;32m-rw-r--r--\x1b[0m 1 user group \x1b[1;34m4096\x1b[0m " +
		"Jan 01 12:00 \x1b[38;5;39msomefile_name.txt\x1b[0m\r\n")
	out := make([]byte, 0, n+len(line))
	for len(out) < n {
		out = append(out, line...)
	}
	return out
}

// BenchmarkPurfecTermFeed measures the "after the wire" cost: how fast the host
// applies bytes to the terminal grid (escape parsing + grid mutation + the
// per-chunk Update), fed in 4 KB chunks like the wire delivers them. Reported
// as MB/s via b.SetBytes.
func BenchmarkPurfecTermFeed(b *testing.B) {
	data := termWorkload(1 << 20) // 1 MB
	t := NewPurfecTerm()
	if t.terminal == nil {
		b.Skip("terminal unavailable")
	}
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for off := 0; off < len(data); off += 4096 {
			end := off + 4096
			if end > len(data) {
				end = len(data)
			}
			t.Feed(data[off:end])
		}
	}
}

// BenchmarkPurfecTermFeedPlain is the parser's best case: plain text, no
// escapes - a baseline to compare the escape-heavy case against.
func BenchmarkPurfecTermFeedPlain(b *testing.B) {
	line := []byte("the quick brown fox jumps over the lazy dog 0123456789\r\n")
	data := make([]byte, 0, (1<<20)+len(line))
	for len(data) < (1 << 20) {
		data = append(data, line...)
	}
	t := NewPurfecTerm()
	if t.terminal == nil {
		b.Skip("terminal unavailable")
	}
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for off := 0; off < len(data); off += 4096 {
			end := off + 4096
			if end > len(data) {
				end = len(data)
			}
			t.Feed(data[off:end])
		}
	}
}

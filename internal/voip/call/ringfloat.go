package call

// ringFloat32 is a fixed-capacity ring buffer for PCM samples.
//
// Why a ring (not a slice + append + slice-shift):
//
//   - The previous capture buffer used `append(buf, data...)` to enqueue and
//     `buf = buf[frameSize:]` every 20ms to dequeue one frame. With a 120s
//     cap (≈5.76M float32 = 23 MiB at 48 kHz) the slice-shift forced Go to
//     drop the first frameSize samples by *re-slicing the header* but the
//     backing array kept growing — every subsequent `append` past capacity
//     reallocated and copied the whole tail. On a busy URA that churned
//     hundreds of MB/s through the allocator and starved the send loop.
//
//   - A ring touches O(frameSize) bytes per frame regardless of how much
//     audio is queued, never reallocates after construction, and produces
//     zero GC pressure on the hot path.
//
// The buffer is *not* safe for concurrent use; callers must hold their own
// lock (CallManager already serializes capture/send under m.mu).
type ringFloat32 struct {
	buf  []float32
	head int // read index
	tail int // write index
	size int // number of valid samples
}

func newRingFloat32(capacity int) *ringFloat32 {
	if capacity <= 0 {
		capacity = 1
	}
	return &ringFloat32{buf: make([]float32, capacity)}
}

func (r *ringFloat32) Cap() int  { return len(r.buf) }
func (r *ringFloat32) Len() int  { return r.size }
func (r *ringFloat32) Free() int { return len(r.buf) - r.size }

// Write appends src to the ring. If src is larger than the free space, the
// *oldest* samples are dropped to make room (matching the previous behaviour
// of truncating the front of the capture buffer when it overflowed).
func (r *ringFloat32) Write(src []float32) {
	if len(src) == 0 {
		return
	}
	cap := len(r.buf)
	// If src alone is bigger than the ring, keep only the tail.
	if len(src) >= cap {
		copy(r.buf, src[len(src)-cap:])
		r.head = 0
		r.tail = 0
		r.size = cap
		return
	}
	// Drop oldest samples if not enough free space.
	if overflow := r.size + len(src) - cap; overflow > 0 {
		r.head = (r.head + overflow) % cap
		r.size -= overflow
	}
	// Two-step copy across the wrap point.
	n := copy(r.buf[r.tail:], src)
	if n < len(src) {
		copy(r.buf, src[n:])
	}
	r.tail = (r.tail + len(src)) % cap
	r.size += len(src)
}

// ReadInto fills dst with up to len(dst) samples and returns how many were
// copied. The samples are *consumed* (head advances). If fewer than len(dst)
// samples are available, the rest of dst is left untouched — the caller is
// expected to have pre-filled it with silence.
func (r *ringFloat32) ReadInto(dst []float32) int {
	if r.size == 0 || len(dst) == 0 {
		return 0
	}
	n := len(dst)
	if n > r.size {
		n = r.size
	}
	cap := len(r.buf)
	first := cap - r.head
	if first > n {
		first = n
	}
	copy(dst[:first], r.buf[r.head:r.head+first])
	if n > first {
		copy(dst[first:n], r.buf[:n-first])
	}
	r.head = (r.head + n) % cap
	r.size -= n
	return n
}

// Reset drops all queued samples without releasing the backing array.
func (r *ringFloat32) Reset() {
	r.head = 0
	r.tail = 0
	r.size = 0
}

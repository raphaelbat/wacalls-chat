package media

func Downsample48to16(in []float32) []float32 {
	out := make([]float32, len(in)/3)
	for i := range out {

		j := i * 3
		out[i] = (in[j] + in[j+1] + in[j+2]) / 3
	}
	return out
}

func Upsample16to48(in []float32) []float32 {
	if len(in) == 0 {
		return nil
	}
	out := make([]float32, len(in)*3)
	for i := 0; i < len(in); i++ {
		cur := in[i]
		var next float32
		if i+1 < len(in) {
			next = in[i+1]
		} else {
			next = cur
		}
		out[i*3] = cur
		out[i*3+1] = cur + (next-cur)/3
		out[i*3+2] = cur + 2*(next-cur)/3
	}
	return out
}

func NormalizeFrame(pcm []float32, n int) []float32 {
	if len(pcm) == n {
		return pcm
	}
	out := make([]float32, n)
	copy(out, pcm)
	return out
}

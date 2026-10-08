package ionet

func mapSlice[A, B any](in []A, f func(A, int) B) []B {
	out := make([]B, len(in))
	for i, v := range in {
		out[i] = f(v, i)
	}
	return out
}
func filterMap[A, B any](in []A, f func(A, int) (B, bool)) []B {
	out := make([]B, 0)
	for i, v := range in {
		if b, ok := f(v, i); ok {
			out = append(out, b)
		}
	}
	return out
}
func sumBy[A any](in []A, f func(A) int) int {
	out := 0
	for _, v := range in {
		out += f(v)
	}
	return out
}
func mapValues[K comparable, A, B any](in map[K]A, f func(A, K) B) map[K]B {
	out := make(map[K]B, len(in))
	for k, v := range in {
		out[k] = f(v, k)
	}
	return out
}

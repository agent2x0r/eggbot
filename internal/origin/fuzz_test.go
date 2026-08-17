package origin

import "testing"

func FuzzIsCTCP(f *testing.F) {
	f.Add("\x01VERSION\x01")
	f.Add("hello")
	f.Fuzz(func(t *testing.T, s string) {
		_, _, _ = IsCTCP(s)
		_, _, _ = Bang(s)
		_, _ = Addressed("eggbot", s)
		_ = IsChannel(s)
	})
}

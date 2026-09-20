package main
import ("fmt"; "strings")
func main() {
	var b strings.Builder
	for i := 0; i < 3000; i++ {
		fmt.Fprintf(&b, "// line %d: the quick brown fox jumps over the lazy dog and keeps running\n", i)
	}
	fmt.Print(b.String())
}

package tools

// All returns every shipped tool definition in display order.
func All() []Definition {
	out := readTools()
	out = append(out, contextTools()...)
	out = append(out, writeTools()...)
	return out
}

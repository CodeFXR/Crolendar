package main

func truncate(s string, max int) string {
	if max <= 0 { return "" }
	if len(s) > max {
		return s[:max]
	}
	return s
}

package umpire

import "strconv"

func strconvFormatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

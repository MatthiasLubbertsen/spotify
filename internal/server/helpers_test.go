package server_test

import (
	"encoding/base64"
	"strconv"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func itoa(v any) string { return strconv.Itoa(int(v.(float64))) }

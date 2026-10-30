package app

import "encoding/json"

func decodeInfo(b []byte, v any) error { return json.Unmarshal(b, v) }

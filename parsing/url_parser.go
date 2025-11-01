package parsing

import (
	"encoding/base64"
	"errors"
	"strings"

	"github.com/bariiss/hls-proxy/model"
)

func ParseInputUrl(inputString string) (*model.Input, error) {
	// normalize input: trim whitespace, strip common proxy suffixes
	s := strings.TrimSpace(inputString)
	// strip trailing .ts if present (proxy appends this after the encoded string)
	s = strings.TrimSuffix(s, ".ts")
	// strip any trailing slash
	s = strings.TrimRight(s, "/")

	decodedBytes, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, errors.New("invalid base64 input")
	}

	parts := strings.Split(string(decodedBytes), "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}

	if len(parts) == 0 || parts[0] == "" {
		return nil, errors.New("empty input")
	}

	out := &model.Input{
		Encoded: s,
		Url:     parts[0],
	}

	if len(parts) > 1 {
		out.Referer = parts[1]
	}
	if len(parts) > 2 {
		out.Origin = parts[2]
	}

	return out, nil
}

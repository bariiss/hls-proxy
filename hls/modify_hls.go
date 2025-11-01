package hls

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/bariiss/hls-proxy/config"
	"github.com/bariiss/hls-proxy/model"
	"github.com/cristalhq/base64"
)

var counter atomic.Int32
var re = regexp.MustCompile(`(?i)URI=["']([^"']+)["']`)

func ModifyM3u8(m3u8 string, host_url *url.URL, prefetcher *Prefetcher, input *model.Input, requestHost string) (string, error) {
	var newManifest = strings.Builder{}
	var host = resolveProxyHost(requestHost)
	manifestKey := input.Encoded
	if manifestKey == "" {
		manifestKey = input.Url
	}

	parentPath := path.Dir(host_url.Path)
	host_url.Path = parentPath
	host_url.RawQuery = ""
	host_url.Fragment = ""

	parentUrl := strings.TrimSuffix(host_url.String(), "/")

	masterProxyUrl := "http://" + host + "/"

	//if user wants https, we should use it
	if model.Configuration.UseHttps {
		masterProxyUrl = "https://" + host + "/"
	}

	newManifest.Grow(len(m3u8))
	if strings.Contains(m3u8, "RESOLUTION=") {
		manifestAddr := masterProxyUrl
		for line := range strings.SplitSeq(strings.TrimRight(m3u8, "\n"), "\n") {
			if len(line) == 0 {
				continue
			}

			if line[0] == '#' {
				if strings.HasPrefix(line, "#EXT-X-MEDIA") {
					handleUriTag(line, parentUrl, input, &newManifest, masterProxyUrl)
					newManifest.WriteString("\n")
					continue
				}

				newManifest.WriteString(line)
				newManifest.WriteString("\n")
				continue
			}

			if strings.TrimSpace(line) == "" {
				continue
			}

			AddProxyUrl(manifestAddr, line, true, parentUrl, &newManifest, input)
			newManifest.WriteString("\n")
		}

		return newManifest.String(), nil
	}

	//most likely a master playlist containing the video elements
	history := getManifestHistory(manifestKey)

	var headerLines []string
	mediaSequenceIndex := -1
	var decryptionKey string
	var hasSequence bool
	var currentSequence int
	var currentIV int
	var segmentTags []string
	var newSegments []*manifestSegment
	endList := false

	tsAddr := masterProxyUrl

	lines := strings.SplitSeq(strings.TrimRight(m3u8, "\n"), "\n")
	for line := range lines {
		if len(line) == 0 {
			continue
		}

		if line[0] == '#' {
			switch {
			case strings.HasPrefix(line, "#EXT-X-ENDLIST"):
				endList = true
			case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE"):
				_, value, found := strings.Cut(line, ":")
				if !found {
					return "", errors.New("invalid #EXT-X-MEDIA-SEQUENCE tag")
				}
				sequenceNumber, err := strconv.Atoi(value)
				if err != nil {
					return "", err
				}
				currentSequence = sequenceNumber
				currentIV = sequenceNumber
				hasSequence = true
				headerLines = append(headerLines, line)
				mediaSequenceIndex = len(headerLines) - 1
			case strings.HasPrefix(line, "#EXT-X-KEY"):
				if model.Configuration.DecryptSegments {
					_, proxyUrl := getUrlForEmbeddedEntry(line, parentUrl)
					if proxyUrl == "" {
						return "", errors.New("missing key URI")
					}
					resp, err := http.Get(proxyUrl)
					if err != nil {
						return "", err
					}
					body, err := io.ReadAll(resp.Body)
					resp.Body.Close()
					if err != nil {
						return "", err
					}
					decryptionKey = base64.URLEncoding.EncodeToString(body)
					break
				}

				var tagBuilder strings.Builder
				handleUriTag(line, parentUrl, input, &tagBuilder, masterProxyUrl)
				segmentTags = append(segmentTags, tagBuilder.String())
			case isPlaylistHeader(line):
				headerLines = append(headerLines, line)
			default:
				segmentTags = append(segmentTags, line)
			}
			continue
		}

		if !hasSequence {
			currentSequence = len(newSegments)
			currentIV = currentSequence
			hasSequence = true
		}

		entry := &manifestSegment{
			Sequence:      currentSequence,
			Tags:          append([]string(nil), segmentTags...),
			Line:          line,
			ClipURL:       joinURL(parentUrl, line),
			HasKey:        decryptionKey != "",
			DecryptionKey: decryptionKey,
			IV:            currentIV,
		}

		newSegments = append(newSegments, entry)
		if decryptionKey != "" {
			currentIV++
		}
		currentSequence++
		segmentTags = segmentTags[:0]
	}

	combined := history.merge(newSegments, config.Settings.SegmentCount)

	playlistId := derivePlaylistID(history, manifestKey)
	strId := playlistId
	pidParam := url.QueryEscape(strId)

	clipUrls := make([]string, 0, len(combined))

	if len(combined) > 0 {
		seqLine := "#EXT-X-MEDIA-SEQUENCE:" + strconv.Itoa(combined[0].Sequence)
		if mediaSequenceIndex >= 0 {
			headerLines[mediaSequenceIndex] = seqLine
		}
		if mediaSequenceIndex < 0 {
			headerLines = append([]string{seqLine}, headerLines...)
		}
	}

	// ensure we always include #EXTM3U at top
	if len(headerLines) == 0 || headerLines[0] != "#EXTM3U" {
		headerLines = append([]string{"#EXTM3U"}, headerLines...)
	}

	for _, header := range headerLines {
		if header == "" {
			continue
		}
		newManifest.WriteString(header)
		newManifest.WriteString("\n")
	}

	for _, entry := range combined {
		clipUrls = append(clipUrls, entry.ClipURL)

		for _, tag := range entry.Tags {
			if tag == "" {
				continue
			}
			newManifest.WriteString(tag)
			newManifest.WriteString("\n")
		}

		AddProxyUrl(tsAddr, entry.Line, false, parentUrl, &newManifest, input)
		newManifest.WriteString("?pId=" + pidParam)
		if entry.HasKey {
			newManifest.WriteString("&key=" + entry.DecryptionKey)
			newManifest.WriteString("&iv=" + strconv.Itoa(entry.IV))
		}
		newManifest.WriteString("\n")
	}

	if endList {
		newManifest.WriteString("#EXT-X-ENDLIST\n")
	}

	prefetcher.AddPlaylistToCache(strId, clipUrls)

	return newManifest.String(), nil
}

func resolveProxyHost(requestHost string) string {
	host := strings.TrimSpace(model.Configuration.Host)
	if (host == "" || host == "0.0.0.0" || host == "[::]") && requestHost != "" {
		return requestHost
	}
	return host
}

func handleUriTag(line string, parentUrl string, input *model.Input, newManifest *strings.Builder, masterProxyUrl string) {
	original, proxyUrl := getUrlForEmbeddedEntry(line, parentUrl)
	if original == "" {
		newManifest.WriteString(line)
		return
	}

	if input.Referer != "" {
		proxyUrl += "|" + input.Referer
	}
	if input.Origin != "" {
		proxyUrl += "|" + input.Origin
	}
	encodedProxyUrl := base64.StdEncoding.EncodeToString([]byte(proxyUrl))
	newManifest.WriteString(strings.Replace(line, original, masterProxyUrl+encodedProxyUrl, 1))
}

func getUrlForEmbeddedEntry(url string, parentUrl string) (string, string) {
	match := re.FindStringSubmatch(url)
	if len(match) < 2 {
		return "", ""
	}

	uri := match[1]
	if isAbsoluteURL(uri) {
		return uri, uri
	}
	return uri, joinURL(parentUrl, uri)
}

func AddProxyUrl(baseAddr string, url string, isManifest bool, parentUrl string, builder *strings.Builder, input *model.Input) {

	proxyUrl := url
	if input.Referer != "" {
		proxyUrl += "|" + input.Referer
	}
	if input.Origin != "" {
		proxyUrl += "|" + input.Origin
	}
	builder.WriteString(baseAddr)
	if isAbsoluteURL(url) {
		builder.WriteString(base64.StdEncoding.EncodeToString([]byte(proxyUrl)))
		return
	}
	builder.WriteString(base64.StdEncoding.EncodeToString([]byte(joinURL(parentUrl, proxyUrl))))
}

func isAbsoluteURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

func derivePlaylistID(history *manifestHistory, manifestKey string) string {
	current := history.currentPlaylistID()
	if current != "" {
		return current
	}

	seed := manifestKey
	if seed == "" {
		seed = strconv.Itoa(int(counter.Add(1)))
	}

	return history.ensurePlaylistID(seed)
}

func joinURL(base, rel string) string {
	base = strings.TrimRight(base, "/")
	rel = strings.TrimLeft(rel, "/")
	if base == "" {
		return rel
	}
	if rel == "" {
		return base
	}
	return base + "/" + rel
}

func isPlaylistHeader(line string) bool {
	switch {
	case strings.HasPrefix(line, "#EXTM3U"):
		return true
	case strings.HasPrefix(line, "#EXT-X-VERSION"):
		return true
	case strings.HasPrefix(line, "#EXT-X-TARGETDURATION"):
		return true
	case strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE"):
		return true
	case strings.HasPrefix(line, "#EXT-X-INDEPENDENT-SEGMENTS"):
		return true
	case strings.HasPrefix(line, "#EXT-X-SERVER-CONTROL"):
		return true
	case strings.HasPrefix(line, "#EXT-X-ALLOW-CACHE"):
		return true
	default:
		return false
	}
}

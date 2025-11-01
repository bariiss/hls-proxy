package hls

import (
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

/*
*	Very barebones m3u8 parser that will replace the URI inside the manifest with a proxy url
*	It only supports a subset of the m3u8 tags and will not work with all m3u8 files
*   It should probably be replaced with a proper m3u8 parser
 */

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

	masterProxyUrl := ""
	//if user wants https, we should use it
	if model.Configuration.UseHttps {
		masterProxyUrl = "https://" + host + "/"
	} else {
		masterProxyUrl = "http://" + host + "/"
	}

	newManifest.Grow(len(m3u8))
	if strings.Contains(m3u8, "RESOLUTION=") {
		manifestAddr := masterProxyUrl
		for line := range strings.SplitSeq(strings.TrimRight(m3u8, "\n"), "\n") {
			if len(line) == 0 {

				continue
			}
			if line[0] == '#' {
				//check for known tags and use regex to replace URI inside
				if strings.HasPrefix(line, "#EXT-X-MEDIA") {

					handleUriTag(line, parentUrl, input, &newManifest, masterProxyUrl)
				} else {
					newManifest.WriteString(line)
				}
			} else if len(strings.TrimSpace(line)) > 0 {

				AddProxyUrl(manifestAddr, line, true, parentUrl, &newManifest, input)

			}
			newManifest.WriteString("\n")
		}
	} else {
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

		lines := strings.Split(strings.TrimRight(m3u8, "\n"), "\n")
		for _, line := range lines {
			if len(line) == 0 {
				continue
			}

			if line[0] == '#' {
				switch {
				case strings.HasPrefix(line, "#EXT-X-ENDLIST"):
					endList = true
				case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE"):
					sequenceNumber, err := strconv.Atoi(strings.Split(line, ":")[1])
					if err != nil {
						return "", err
					}
					currentSequence = sequenceNumber
					currentIV = sequenceNumber
					hasSequence = true
					headerLines = append(headerLines, line)
					mediaSequenceIndex = len(headerLines) - 1
				case strings.HasPrefix(line, "#EXT-X-KEY") && model.Configuration.DecryptSegments:
					_, proxyUrl := getUrlForEmbeddedEntry(line, parentUrl)
					resp, err := http.Get(proxyUrl)
					if err != nil {
						return "", err
					}
					defer resp.Body.Close()
					body, err := io.ReadAll(resp.Body)
					if err != nil {
						return "", err
					}
					decryptionKey = base64.URLEncoding.EncodeToString(body)
				case strings.HasPrefix(line, "#EXT-X-KEY"):
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

		playlistId := history.currentPlaylistID()
		if playlistId == "" {
			playlistId = manifestKey
			if playlistId == "" {
				playlistId = strconv.Itoa(int(counter.Add(1)))
			}
			playlistId = history.ensurePlaylistID(playlistId)
		}
		strId := playlistId
		pidParam := url.QueryEscape(strId)

		clipUrls := make([]string, 0, len(combined))

		if len(combined) > 0 {
			if mediaSequenceIndex >= 0 {
				headerLines[mediaSequenceIndex] = "#EXT-X-MEDIA-SEQUENCE:" + strconv.Itoa(combined[0].Sequence)
			} else {
				headerLines = append([]string{"#EXT-X-MEDIA-SEQUENCE:" + strconv.Itoa(combined[0].Sequence)}, headerLines...)
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

	}

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
	if strings.HasPrefix(match[1], "http") || strings.HasPrefix(match[1], "https") {
		return match[1], match[1]
	} else {
		return match[1], joinURL(parentUrl, match[1])
	}
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
	if strings.HasPrefix(url, "http") || strings.HasPrefix(url, "https") {
		builder.WriteString(base64.StdEncoding.EncodeToString([]byte(proxyUrl)))
	} else {
		builder.WriteString(base64.StdEncoding.EncodeToString([]byte(joinURL(parentUrl, proxyUrl))))
	}
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

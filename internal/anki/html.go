package anki

import (
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var (
	soundRe  = regexp.MustCompile(`\[sound:([^\]]+)\]`)
	spacesRe = regexp.MustCompile(`[ \t]{2,}`)
	blankRe  = regexp.MustCompile(`\n{3,}`)
	wsRe     = regexp.MustCompile(`[\s\x{00a0}]+`)
)

var blockTags = map[string]bool{"div": true, "p": true, "li": true, "tr": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "blockquote": true, "ul": true, "ol": true, "table": true, "section": true, "article": true, "header": true, "footer": true, "dl": true, "dt": true, "dd": true}

// skipTags lose their content entirely.
var skipTags = map[string]bool{"script": true, "style": true, "head": true, "title": true, "rp": true, "template": true}

// mediaName is the plain file name of an Anki media reference ("" when it is
// not a local file).
func mediaName(src string) string {
	src = strings.TrimSpace(src)
	if src == "" || strings.HasPrefix(src, "data:") {
		return ""
	}
	if u, err := url.PathUnescape(src); err == nil {
		src = u
	}
	return path.Base(strings.ReplaceAll(src, "\\", "/"))
}

type list struct {
	ordered bool
	n       int
}

// HTMLToText turns an Anki field's HTML into the app's plain "markdown-lite"
// text: **bold**, *italic*, `code`, fenced blocks, "- " lists, ![](media/x)
// images and [sound:x] references; everything else becomes plain text.
func HTMLToText(s string) string {
	if !strings.ContainsAny(s, "<&") {
		return tidy(soundNames(wsRe.ReplaceAllString(s, " ")))
	}
	z := html.NewTokenizer(strings.NewReader(s))
	var b strings.Builder
	skip, pre := 0, 0
	var lists []list
	cell := 0
	nl := func() {
		str := b.String()
		if str != "" && !strings.HasSuffix(str, "\n") {
			b.WriteByte('\n')
		}
	}
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		name, hasAttr := z.TagName()
		tag := string(name)
		switch tt {
		case html.TextToken:
			if skip > 0 {
				continue
			}
			t := string(z.Text())
			if pre == 0 {
				t = wsRe.ReplaceAllString(t, " ")
			}
			b.WriteString(t)
		case html.StartTagToken, html.SelfClosingTagToken:
			if skipTags[tag] {
				if tt == html.StartTagToken {
					skip++
				}
				continue
			}
			if skip > 0 {
				continue
			}
			attrs := map[string]string{}
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				attrs[string(k)] = string(v)
			}
			switch tag {
			case "b", "strong":
				b.WriteString("**")
			case "i", "em":
				b.WriteString("*")
			case "code":
				if pre == 0 {
					b.WriteString("`")
				}
			case "br":
				b.WriteByte('\n')
			case "hr":
				nl()
				b.WriteString("---\n")
			case "pre":
				nl()
				b.WriteString("```\n")
				pre++
			case "rt":
				b.WriteString("(")
			case "img":
				src := strings.TrimSpace(attrs["src"])
				low := strings.ToLower(src)
				switch {
				case strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://"):
					b.WriteString("![](" + src + ")")
				default:
					if n := mediaName(src); n != "" {
						b.WriteString("![](media/" + n + ")")
					}
				}
			case "ul", "ol":
				nl()
				lists = append(lists, list{ordered: tag == "ol"})
			case "li":
				nl()
				if len(lists) > 0 && lists[len(lists)-1].ordered {
					lists[len(lists)-1].n++
					b.WriteString(strconv.Itoa(lists[len(lists)-1].n) + ". ")
				} else {
					b.WriteString("- ")
				}
			case "tr":
				nl()
				cell = 0
			case "td", "th":
				if cell > 0 {
					b.WriteString(" | ")
				}
				cell++
			default:
				if blockTags[tag] {
					nl()
				}
			}
		case html.EndTagToken:
			if skipTags[tag] {
				if skip > 0 {
					skip--
				}
				continue
			}
			if skip > 0 {
				continue
			}
			switch tag {
			case "b", "strong":
				b.WriteString("**")
			case "i", "em":
				b.WriteString("*")
			case "code":
				if pre == 0 {
					b.WriteString("`")
				}
			case "pre":
				if pre > 0 {
					pre--
				}
				nl()
				b.WriteString("```\n")
			case "rt":
				b.WriteString(")")
			case "ul", "ol":
				if len(lists) > 0 {
					lists = lists[:len(lists)-1]
				}
				nl()
			default:
				if blockTags[tag] {
					nl()
				}
			}
		}
	}
	return tidy(soundNames(b.String()))
}

// soundNames keeps only the file name inside [sound:…].
func soundNames(s string) string {
	return soundRe.ReplaceAllStringFunc(s, func(m string) string {
		n := mediaName(soundRe.FindStringSubmatch(m)[1])
		if n == "" {
			return ""
		}
		return "[sound:" + n + "]"
	})
}

// tidy collapses spaces, trims lines (not inside ``` fences) and keeps at
// most one blank line in a row.
func tidy(s string) string {
	s = strings.ReplaceAll(s, " ", " ")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	fence := false
	for i, l := range lines {
		if strings.TrimSpace(l) == "```" {
			fence = !fence
			lines[i] = "```"
			continue
		}
		if fence {
			lines[i] = strings.TrimRight(l, " \t")
			continue
		}
		lines[i] = strings.TrimSpace(spacesRe.ReplaceAllString(l, " "))
	}
	s = strings.Join(lines, "\n")
	s = blankRe.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

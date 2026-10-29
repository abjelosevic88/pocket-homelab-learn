package anki

import "testing"

func TestHTMLToText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain text", "plain text"},
		{"a  &amp;  b&nbsp;c", "a & b c"},
		{"<b>bold</b> and <i>it</i> and <strong>s</strong> <em>e</em> <u>u</u>", "**bold** and *it* and **s** *e* u"},
		{"one<br>two<br/>three", "one\ntwo\nthree"},
		{"<div>a</div><div>b</div>", "a\nb"},
		{"<p>a</p><p>b</p>", "a\nb"},
		{"<ul><li>x</li><li>y</li></ul>", "- x\n- y"},
		{"<ol><li>x</li><li>y</li></ol>after", "1. x\n2. y\nafter"},
		{`<img src="cat%20one.jpg"> <img src="data:image/png;base64,xx"> <img src="https://e.com/a.png">`, "![](media/cat one.jpg) ![](https://e.com/a.png)"},
		{`<img src="sub/dir/dog.png">`, "![](media/dog.png)"},
		{"run <code>ls -l</code>", "run `ls -l`"},
		{"<pre>a\n  b</pre>", "```\na\n  b\n```"},
		{"<ruby>漢字<rp>(</rp><rt>かんじ</rt><rp>)</rp></ruby>", "漢字(かんじ)"},
		{"<table><tr><th>a</th><th>b</th></tr><tr><td>1</td><td>2</td></tr></table>", "a | b\n1 | 2"},
		{"a<hr>b", "a\n---\nb"},
		{"<style>.x{}</style><script>alert(1)</script>ok", "ok"},
		{"<span style=\"color:red\">red</span> [sound:dir/hello.mp3]", "red [sound:hello.mp3]"},
		{"{{c1::Paris::capital}} is in <b>France</b>", "{{c1::Paris::capital}} is in **France**"},
		{"<div>a</div><br><br><br><br><div>b</div>", "a\n\nb"},
		{"<div>  spaced   out  </div>", "spaced out"},
		{"&lt;tag&gt;", "<tag>"},
	}
	for _, c := range cases {
		if got := HTMLToText(c.in); got != c.want {
			t.Errorf("HTMLToText(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestDeckName(t *testing.T) {
	for in, want := range map[string]string{"A::B": "A › B", "A\x1fB\x1fC": "A › B › C", "": "Default", "Solo": "Solo"} {
		if got := deckName(in); got != want {
			t.Errorf("deckName(%q) = %q, want %q", in, got, want)
		}
	}
}

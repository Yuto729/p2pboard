package cli

import "strings"

// parseMentions は本文から @name 形式のメンションを抽出する。
// 例: "@reviewer これ見て @bob" -> ["reviewer", "bob"]
// 記号や句読点で終わる場合も名前部分だけを取り出す。
func parseMentions(body string) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, tok := range strings.Fields(body) {
		if !strings.HasPrefix(tok, "@") || len(tok) == 1 {
			continue
		}
		name := strings.TrimFunc(tok[1:], func(r rune) bool {
			// 名前として許すのは英数字・ハイフン・アンダースコアのみ。
			return !(r == '-' || r == '_' ||
				(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
				(r >= '0' && r <= '9'))
		})
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

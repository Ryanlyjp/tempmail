package store

import (
	"crypto/rand"
	"errors"
	"math/big"
	"regexp"
	"strings"
)

// 子域规则（与前端约束一致）
const (
	SubdomainMin     = 2
	SubdomainMax     = 8
	SubdomainDefault = 5
	subdomainChars   = "abcdefghijklmnopqrstuvwxyz0123456789"
)

var subdomainRe = regexp.MustCompile(`^[a-z0-9]{2,8}$`)

// ClampSubdomainLength 把任意输入夹到 [2,8]，零或越界返回默认 5
func ClampSubdomainLength(n int) int {
	if n < SubdomainMin || n > SubdomainMax {
		return SubdomainDefault
	}
	return n
}

// GenerateRandomSubdomain 生成 length 位随机子域；length 越界自动 clamp
func GenerateRandomSubdomain(length int) string {
	length = ClampSubdomainLength(length)
	out := make([]byte, length)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(subdomainChars))))
		if err != nil {
			// 极端情况下退回 0 索引；不应在生产环境出现
			out[i] = subdomainChars[0]
			continue
		}
		out[i] = subdomainChars[n.Int64()]
	}
	return string(out)
}

// ValidateSubdomain 校验自定义子域格式；返回归一化（小写 + 去空白）后的字符串
func ValidateSubdomain(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if !subdomainRe.MatchString(s) {
		return "", errors.New("子域格式无效（仅允许 2-8 位 a-z 0-9）")
	}
	return s, nil
}

// SplitFullDomain 把 fullDomain（如 "xx.bb.cc.dd"）按候选 base 列表拆成 (sub, base)。
// - 完全匹配时 sub="" base=匹配项
// - 后缀匹配时（fullDomain == "xx" + "." + base）且 sub 部分不含点，sub=该段
// - 找不到则返回 ok=false
// 调用方应只把"开启子域的活跃域"放进 baseCandidates，避免误把 a.example.com 拆给 example.com。
func SplitFullDomain(fullDomain string, baseCandidates []string) (sub, base string, ok bool) {
	fullDomain = strings.ToLower(strings.TrimSpace(fullDomain))
	for _, b := range baseCandidates {
		b = strings.ToLower(strings.TrimSpace(b))
		if b == "" {
			continue
		}
		if fullDomain == b {
			return "", b, true
		}
		if strings.HasSuffix(fullDomain, "."+b) {
			prefix := strings.TrimSuffix(fullDomain, "."+b)
			if prefix != "" && !strings.Contains(prefix, ".") {
				return prefix, b, true
			}
		}
	}
	return "", "", false
}

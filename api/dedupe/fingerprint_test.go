package dedupe

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestFingerprintStableAcrossWhitespaceAndUnicode(t *testing.T) {
	a := "Cafe\u0301\n算法分析\twith measurements"
	b := "Café 算法分析 with measurements"
	if !bytes.Equal(Signature(a), Signature(b)) {
		t.Fatal("normalization must be deterministic")
	}
	if len(Signature(a)) != 1024 || len(Buckets(Signature(a))) != 16 {
		t.Fatal("unexpected signature shape")
	}
	if Buckets([]byte{1}) != nil {
		t.Fatal("corrupt signatures must not create buckets")
	}
}
func TestVerifyPreservesIndependentContent(t *testing.T) {
	body := ""
	for i := 0; i < 40; i++ {
		body += fmt.Sprintf("章节 %d：缓存命中率需要通过真实请求验证，同时分析延迟、内存占用和失效策略。", i)
	}
	for _, test := range []struct {
		name, a, b string
		want       bool
	}{
		{"exact", body, body, true},
		{"minor edit", body, strings.Replace(body, "真实请求", "实际请求", 1), true},
		{"numeric changes", body + " measured 123", body + " measured 124", false},
		{"sign changes", body + " measured -123", body + " measured +123", false},
		{"short", "a short independent article", "a short independent articlf", false},
		{"same topic", body, strings.Repeat("缓存系统需要考虑分布式一致性，并以不同架构说明写入和读取的权衡。", 12), false},
		{"case sensitive code", strings.Repeat("Value := Request.Header.Get(Name); ", 12), strings.Repeat("value := request.header.get(name); ", 12), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, ok := Verify(test.a, test.b)
			if ok != test.want {
				t.Fatalf("match=%v want=%v", ok, test.want)
			}
		})
	}
}
func TestNearDuplicateRetrieval(t *testing.T) {
	// Candidate generation, including multilingual bodies, must retrieve these verified pairs.
	for _, body := range []string{strings.Repeat("正文描述算法的机制和实际测试结果，包含局限和结论。", 20), strings.Repeat("Measured queue latency and memory usage reveal the operational tradeoffs. ", 20)} {
		modified := body + "."
		if _, ok := Verify(body, modified); !ok {
			t.Fatal("fixture must be a verified duplicate")
		}
		first, second := Buckets(Signature(body)), Buckets(Signature(modified))
		found := false
		for i := range first {
			if first[i] == second[i] {
				found = true
			}
		}
		if !found {
			t.Fatal("LSH missed verified pair")
		}
	}
}
func BenchmarkFingerprint(b *testing.B) {
	body := strings.Repeat("A realistic paragraph about queue processing, failures, and operational tradeoffs. ", 100)
	b.ReportAllocs()
	for b.Loop() {
		Signature(body)
	}
}

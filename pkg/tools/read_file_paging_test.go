package tools

import (
	"strings"
	"testing"
)

func numberedLines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString("line")
		b.WriteString(strings.Repeat("x", i%3))
		b.WriteString("\n")
	}
	return b.String()
}

func TestSliceFileLines(t *testing.T) {
	small := "a\nb\nc\n"

	t.Run("小文件原样返回", func(t *testing.T) {
		got, err := sliceFileLines(small, 0, 0)
		if err != nil || got != small {
			t.Fatalf("got %q, err %v", got, err)
		}
	})

	t.Run("无结尾换行原样返回", func(t *testing.T) {
		got, _ := sliceFileLines("a\nb", 0, 0)
		if got != "a\nb" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("空文件", func(t *testing.T) {
		got, err := sliceFileLines("", 0, 0)
		if err != nil || got != "" {
			t.Fatalf("got %q, err %v", got, err)
		}
	})

	t.Run("超过默认上限时截断并提示续读", func(t *testing.T) {
		content := numberedLines(defaultReadFileLimit + 10)
		got, err := sliceFileLines(content, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "共 2010 行") || !strings.Contains(got, "offset=2001") {
			t.Errorf("缺少截断提示: %q", got[len(got)-80:])
		}
	})

	t.Run("offset+limit 取中间段", func(t *testing.T) {
		got, err := sliceFileLines("1\n2\n3\n4\n5\n", 2, 2)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(got, "2\n3\n") || !strings.Contains(got, "第 2-3 行，共 5 行") || !strings.Contains(got, "offset=4") {
			t.Errorf("got %q", got)
		}
	})

	t.Run("读到末尾不再提示续读", func(t *testing.T) {
		got, _ := sliceFileLines("1\n2\n3\n", 2, 10)
		if strings.Contains(got, "继续读取") || !strings.Contains(got, "第 2-3 行") {
			t.Errorf("got %q", got)
		}
	})

	t.Run("非法参数", func(t *testing.T) {
		if _, err := sliceFileLines(small, 10, 0); err == nil {
			t.Error("offset 超出总行数应报错")
		}
		if _, err := sliceFileLines(small, -1, 0); err == nil {
			t.Error("负数 offset 应报错")
		}
	})
}

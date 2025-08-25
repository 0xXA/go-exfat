package exfat

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// FormatFileSize 格式化文件大小显示
func FormatFileSize(size int64) string {
	units := []struct {
		unit string
		size int64
	}{
		{"TB", 1 << 40},
		{"GB", 1 << 30},
		{"MB", 1 << 20},
		{"KB", 1 << 10},
		{"B", 1},
	}
	for _, u := range units {
		if size >= u.size {
			if u.unit == "B" {
				return fmt.Sprintf("%d %s", size, u.unit)
			}
			return fmt.Sprintf("%.2f %s", float64(size)/float64(u.size), u.unit)
		}
	}
	return "0 B"
}

// normalizePath 标准化路径，确保使用正斜杠并以斜杠开头
func normalizePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// setFileModTime 设置文件的修改时间
func setFileModTime(path string, modTime time.Time) error {
	return os.Chtimes(path, modTime, modTime)
}

package exfat

import "fmt"

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

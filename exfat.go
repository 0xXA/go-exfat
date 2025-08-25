package exfat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileEntry 表示文件或目录的基本信息
type FileEntry struct {
	Name    string    // 文件/目录名
	Size    int64     // 文件大小（目录为 0）
	IsDir   bool      // 是否为目录
	ModTime time.Time // 修改时间
}

// VHD 表示一个打开的 VHD 文件和其中的 exFAT 文件系统
type VHD struct {
	vhdFile *VHDFile
	exfat   *ExFATFileSystem
}

// OpenVHD 打开一个 VHD 文件并初始化 exFAT 文件系统
func OpenVHD(path string) (*VHD, error) {
	vhdFile, err := OpenVHDFile(path)
	if err != nil {
		return nil, err
	}

	exfat, err := NewExFATFileSystem(vhdFile)
	if err != nil {
		vhdFile.Close()
		return nil, err
	}

	return &VHD{
		vhdFile: vhdFile,
		exfat:   exfat,
	}, nil
}

// Close 关闭 VHD 文件
func (v *VHD) Close() error {
	return v.vhdFile.Close()
}

// ListDir 列出指定路径的目录内容
func (v *VHD) ListDir(path string) ([]FileEntry, error) {
	return v.exfat.ListDir(path)
}

// ReadFile 读取文件内容
func (v *VHD) ReadFile(path string) ([]byte, error) {
	return v.exfat.ReadFile(path)
}

// ExtractFile 提取文件或目录到指定路径
func (v *VHD) ExtractFile(srcPath, destPath string) error {
	srcPath = normalizePath(srcPath)

	entry, err := v.exfat.getEntry(srcPath)
	if err != nil {
		return fmt.Errorf("failed to get entry for %s: %v", srcPath, err)
	}

	if entry.IsDir {
		return v.extractAllRecursive(srcPath, destPath)
	}

	return v.exfat.ExtractFile(srcPath, filepath.Join(destPath, entry.Name))
}

// extractAllRecursive 递归提取目录内容的内部实现
func (v *VHD) extractAllRecursive(srcPath, destPath string) error {
	// 获取当前目录的内容
	entries, err := v.ListDir(srcPath)
	if err != nil {
		return fmt.Errorf("failed to list directory %s: %v", srcPath, err)
	}

	// 确保目标目录存在
	if err := os.MkdirAll(destPath, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %v", destPath, err)
	}

	for _, entry := range entries {
		// 构建源路径和目标路径
		srcFullPath := filepath.Join(srcPath, entry.Name)
		destFullPath := filepath.Join(destPath, entry.Name)

		// 标准化路径分隔符（在 VHD 中使用正斜杠）
		srcFullPath = normalizePath(srcFullPath)

		if entry.IsDir {
			// 创建目录
			if err := os.MkdirAll(destFullPath, 0755); err != nil {
				fmt.Printf("Warning: Failed to create directory %s: %v\n", destFullPath, err)
				continue
			}

			// 尝试递归处理子目录
			err := v.extractAllRecursive(srcFullPath, destFullPath)
			if err != nil {
				// 这可能是空目录或无效簇号的目录，这是正常的
				fmt.Printf("Warning: Directory %s is empty or inaccessible: %v\n", entry.Name, err)
				// 但目录结构已经创建，所以继续处理其他项目
			}
		} else {
			// 处理文件
			if err := v.exfat.ExtractFile(srcFullPath, destFullPath); err != nil {
				fmt.Printf("Warning: Failed to extract file %s: %v\n", srcFullPath, err)
				// 继续处理其他文件，不中断整个提取过程
				continue
			}

			// 设置文件修改时间（如果可用）
			if !entry.ModTime.IsZero() {
				if err := setFileModTime(destFullPath, entry.ModTime); err != nil {
					fmt.Printf("Warning: Failed to set modification time for file %s: %v\n", destFullPath, err)
				}
			}
		}
	}

	return nil
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

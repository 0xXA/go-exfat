package exfat

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
)

// NewExFATFileSystem 创建新的 exFAT 文件系统实例
func NewExFATFileSystem(vhd io.ReaderAt) (*ExFATFileSystem, error) {
	// 读取引导扇区
	bootSectorData := make([]byte, 512)
	_, err := vhd.ReadAt(bootSectorData, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to read boot sector: %v", err)
	}

	bootSector := &ExFATBootSector{}
	err = binary.Read(bytes.NewReader(bootSectorData), binary.LittleEndian, bootSector)
	if err != nil {
		return nil, fmt.Errorf("failed to parse boot sector: %v", err)
	}

	// 验证 exFAT 签名
	if string(bootSector.FileSystemName[:]) != "EXFAT   " {
		return nil, fmt.Errorf("not a valid exFAT filesystem")
	}

	// 计算参数
	bytesPerSector := uint32(1) << bootSector.BytesPerSectorShift
	sectorsPerCluster := uint32(1) << bootSector.SectorsPerClusterShift
	bytesPerCluster := bytesPerSector * sectorsPerCluster

	fs := &ExFATFileSystem{
		vhd:               vhd,
		bootSector:        bootSector,
		bytesPerSector:    bytesPerSector,
		sectorsPerCluster: sectorsPerCluster,
		bytesPerCluster:   bytesPerCluster,
		clusterHeapStart:  uint64(bootSector.ClusterHeapOffset) * uint64(bytesPerSector),
		totalClusters:     bootSector.ClusterCount,
	}

	// 读取 FAT 表
	err = fs.readFAT()
	if err != nil {
		return nil, err
	}

	return fs, nil
}

// readFAT 读取 FAT 表
func (fs *ExFATFileSystem) readFAT() error {
	fatSize := fs.bootSector.FatLength * fs.bytesPerSector
	fatData := make([]byte, fatSize)

	fatOffset := uint64(fs.bootSector.FatOffset) * uint64(fs.bytesPerSector)
	_, err := fs.vhd.ReadAt(fatData, int64(fatOffset))
	if err != nil {
		return fmt.Errorf("failed to read FAT table: %v", err)
	}

	// 解析 FAT 表（每个条目 4 字节）
	entryCount := fatSize / 4
	fs.fat = make([]uint32, entryCount)
	for i := uint32(0); i < entryCount; i++ {
		fs.fat[i] = binary.LittleEndian.Uint32(fatData[i*4 : (i+1)*4])
	}

	return nil
}

// clusterToOffset 将簇号转换为文件偏移
func (fs *ExFATFileSystem) clusterToOffset(cluster uint32) uint64 {
	if cluster < 2 {
		return 0
	}
	return fs.clusterHeapStart + uint64(cluster-2)*uint64(fs.bytesPerCluster)
}

// readClusterChain 读取簇链的数据
func (fs *ExFATFileSystem) readClusterChain(startCluster uint32, size uint64) ([]byte, error) {
	if size == 0 {
		return []byte{}, nil
	}

	// 检查起始簇号是否有效
	if startCluster == 0 || startCluster >= ReservedCluster {
		return nil, fmt.Errorf("invalid start cluster: %d", startCluster)
	}

	data := make([]byte, size)
	offset := uint64(0)
	cluster := startCluster

	for cluster != EndOfClusterChain && offset < size {
		clusterOffset := fs.clusterToOffset(cluster)
		readSize := fs.bytesPerCluster
		if offset+uint64(readSize) > size {
			readSize = uint32(size - offset)
		}

		if _, err := fs.vhd.ReadAt(data[offset:offset+uint64(readSize)], int64(clusterOffset)); err != nil {
			return nil, fmt.Errorf("failed to read cluster %d: %v", cluster, err)
		}

		offset += uint64(readSize) // 获取下一个簇
		cluster = fs.nextValidCluster(cluster)

		// 检查新簇号是否仍然有效
		if cluster >= fs.totalClusters {
			break
		}
	}

	return data, nil
}

// nextValidCluster 获取下一个有效簇号
func (fs *ExFATFileSystem) nextValidCluster(cluster uint32) uint32 {
	if cluster >= uint32(len(fs.fat)) {
		return cluster + 1
	}
	next := fs.fat[cluster]
	if next == EndOfClusterChain || next >= ReservedCluster || next < 2 || next > 0x10000000 {
		return cluster + 1
	}
	return next
}

// ListDir 列出目录内容
func (fs *ExFATFileSystem) ListDir(path string) ([]FileEntry, error) {
	path = normalizePath(path)

	var dirCluster uint32
	if path == "/" || path == "" {
		dirCluster = fs.bootSector.FirstClusterOfRootDir
	} else {
		// 查找目录
		entry, err := fs.getEntry(path)
		if err != nil {
			return nil, err
		}
		if !entry.IsDir {
			return nil, fmt.Errorf("path is not a directory: %s", path)
		}
		dirCluster = entry.cluster
	}

	return fs.readDirectory(dirCluster)
}

// DirEntry 内部目录条目结构
type DirEntry struct {
	Name    string
	Size    int64
	IsDir   bool
	ModTime time.Time
	cluster uint32
}

// getEntry 查找文件或目录条目
func (fs *ExFATFileSystem) getEntry(path string) (*DirEntry, error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		// 根目录
		return &DirEntry{
			Name:    "/",
			IsDir:   true,
			cluster: fs.bootSector.FirstClusterOfRootDir,
		}, nil
	}

	currentCluster := fs.bootSector.FirstClusterOfRootDir
	var targetEntry *DirEntry

	for i, part := range parts {
		if part == "" {
			continue
		}

		dirEntries, err := fs.readDirectoryEntries(currentCluster)
		if err != nil {
			return nil, err
		}

		found := false
		for _, entry := range dirEntries {
			if strings.EqualFold(entry.Name, part) {
				if i == len(parts)-1 {
					// 找到目标
					return entry, nil
				}
				if entry.IsDir {
					currentCluster = entry.cluster
					found = true
					break
				}
			}
		}

		if !found {
			return nil, fmt.Errorf("path not found: %s", path)
		}
	}

	return targetEntry, fmt.Errorf("failed to resolve path: %s", path)
}

// readDirectoryEntries 读取目录内容并返回内部目录条目
func (fs *ExFATFileSystem) readDirectoryEntries(cluster uint32) ([]*DirEntry, error) {
	// 检查簇号是否有效
	if cluster == 0 || cluster >= ReservedCluster || cluster > 0x10000000 {
		return []*DirEntry{}, nil // 返回空列表，表示空目录
	}

	// 读取目录数据
	dirData, err := fs.readClusterChain(cluster, uint64(fs.bytesPerCluster*16)) // 假设目录不超过16个簇
	if err != nil {
		return nil, err
	}

	var entries []*DirEntry
	offset := 0

	for offset+32 <= len(dirData) {
		entryType := dirData[offset]

		// 检查目录结束
		if entryType == EntryTypeEndOfDirectory {
			break
		}

		// 跳过非文件条目
		if entryType != EntryTypeFile {
			offset += 32
			continue
		}

		// 解析文件条目
		fileEntry := &ExFATFileEntry{}
		err := binary.Read(bytes.NewReader(dirData[offset:offset+32]), binary.LittleEndian, fileEntry)
		if err != nil {
			offset += 32
			continue
		}

		// 读取文件信息条目
		offset += 32
		fileInfoEntry := &ExFATFileInfoEntry{}
		err = binary.Read(bytes.NewReader(dirData[offset:offset+32]), binary.LittleEndian, fileInfoEntry)
		if err != nil {
			offset += 32
			continue
		}

		// 读取文件名
		offset += 32
		nameLength := int(fileInfoEntry.NameLength)
		fileName := ""

		for i := 0; i < int(fileEntry.SecondaryCount)-1 && offset+32 <= len(dirData); i++ {
			nameEntry := &ExFATFileNameEntry{}
			err = binary.Read(bytes.NewReader(dirData[offset:offset+32]), binary.LittleEndian, nameEntry)
			if err != nil {
				offset += 32
				continue
			}

			// 转换 UTF-16LE 到字符串
			nameBytes := nameEntry.FileName[:]
			nameRunes := make([]uint16, len(nameBytes)/2)
			for j := 0; j < len(nameRunes); j++ {
				nameRunes[j] = binary.LittleEndian.Uint16(nameBytes[j*2:])
			}

			namePart := string(utf16.Decode(nameRunes))
			fileName += namePart

			if len(fileName) >= nameLength {
				break
			}

			offset += 32
		}

		// 清理文件名（移除空字符）
		fileName = strings.TrimRight(fileName, "\x00")
		if len(fileName) > nameLength {
			fileName = fileName[:nameLength]
		}

		if fileName == "" {
			continue
		}

		// 验证簇号是否有效（对于目录）
		cluster := fileInfoEntry.FirstCluster
		isDir := (fileEntry.FileAttributes & 0x10) != 0

		// 对于目录，检查簇号是否有效
		// exFAT 中 0xFFFFFFF8 及以上表示特殊簇号（坏簇、保留等）
		if isDir && (cluster == 0 || cluster >= ReservedCluster) {
			// 这可能是一个空目录，我们仍然要创建它，但不尝试读取内容
			cluster = 0
		}

		// 对于任何簇号，检查是否合理（不能太大）
		// 一般来说，簇号不应该超过几百万
		if cluster > 0x10000000 { // 约 268M 簇，对于大多数文件系统来说太大了
			if isDir {
				cluster = 0 // 将无效的目录簇设为 0，表示空目录
			} else {
				// 对于文件，跳过有无效簇号的条目
				continue
			}
		}

		entries = append(entries, &DirEntry{
			Name:    fileName,
			Size:    int64(fileInfoEntry.DataLength),
			IsDir:   isDir,
			ModTime: exfatTimeToTime(fileEntry.LastModifiedTimestamp),
			cluster: cluster,
		})
	}

	return entries, nil
}

// readDirectory 读取目录内容
func (fs *ExFATFileSystem) readDirectory(cluster uint32) ([]FileEntry, error) {
	// 检查簇号是否有效
	if cluster == 0 || cluster >= ReservedCluster || cluster > 0x10000000 {
		return []FileEntry{}, nil // 返回空列表，表示空目录
	}

	// 读取目录数据
	dirData, err := fs.readClusterChain(cluster, uint64(fs.bytesPerCluster*16)) // 假设目录不超过16个簇
	if err != nil {
		return nil, err
	}

	var entries []FileEntry
	offset := 0

	for offset < len(dirData) {
		if offset+32 > len(dirData) {
			break
		}

		entryType := dirData[offset]

		// 检查目录结束
		if entryType == EntryTypeEndOfDirectory {
			break
		}

		// 跳过非文件条目
		if entryType != EntryTypeFile {
			offset += 32
			continue
		}

		// 解析文件条目
		fileEntry := &ExFATFileEntry{}
		err := binary.Read(bytes.NewReader(dirData[offset:offset+32]), binary.LittleEndian, fileEntry)
		if err != nil {
			offset += 32
			continue
		}

		// 读取文件信息条目
		offset += 32
		if offset+32 > len(dirData) {
			break
		}

		fileInfoEntry := &ExFATFileInfoEntry{}
		err = binary.Read(bytes.NewReader(dirData[offset:offset+32]), binary.LittleEndian, fileInfoEntry)
		if err != nil {
			offset += 32
			continue
		}

		// 读取文件名
		offset += 32
		nameLength := int(fileInfoEntry.NameLength)
		fileName := ""

		for i := 0; i < int(fileEntry.SecondaryCount)-1; i++ {
			if offset+32 > len(dirData) {
				break
			}

			nameEntry := &ExFATFileNameEntry{}
			err = binary.Read(bytes.NewReader(dirData[offset:offset+32]), binary.LittleEndian, nameEntry)
			if err != nil {
				offset += 32
				continue
			}

			// 转换 UTF-16LE 到字符串
			nameBytes := nameEntry.FileName[:]
			nameRunes := make([]uint16, len(nameBytes)/2)
			for j := 0; j < len(nameRunes); j++ {
				nameRunes[j] = binary.LittleEndian.Uint16(nameBytes[j*2:])
			}

			namePart := string(utf16.Decode(nameRunes))
			fileName += namePart

			if len(fileName) >= nameLength {
				break
			}

			offset += 32
		}

		// 清理文件名（移除空字符）
		fileName = strings.TrimRight(fileName, "\x00")
		if len(fileName) > nameLength {
			fileName = fileName[:nameLength]
		}

		if fileName != "" {
			entry := FileEntry{
				Name:    fileName,
				Size:    int64(fileInfoEntry.DataLength),
				IsDir:   (fileEntry.FileAttributes & 0x10) != 0,
				ModTime: exfatTimeToTime(fileEntry.LastModifiedTimestamp),
			}
			entries = append(entries, entry)
		}
	}

	return entries, nil
}

// exfatTimeToTime 转换 exFAT 时间戳为 Go time.Time
func exfatTimeToTime(timestamp uint32) time.Time {
	if timestamp == 0 {
		return time.Time{}
	}

	date := timestamp >> 16
	tm := timestamp & 0xFFFF

	year := int((date>>9)&0x7F) + 1980
	month := time.Month((date >> 5) & 0x0F)
	day := int(date & 0x1F)
	hour := int((tm >> 11) & 0x1F)
	minute := int((tm >> 5) & 0x3F)
	second := int(tm&0x1F) * 2

	if month < 1 || month > 12 || day < 1 || day > 31 ||
		hour > 23 || minute > 59 || second > 59 {
		return time.Time{}
	}
	return time.Date(year, month, day, hour, minute, second, 0, time.Local)
}

// ReadFile 读取文件内容
func (fs *ExFATFileSystem) ReadFile(path string) ([]byte, error) {
	entry, err := fs.getEntry(path)
	if err != nil {
		return nil, err
	}

	if entry.IsDir {
		return nil, fmt.Errorf("path is a directory, not a file: %s", path)
	}

	return fs.readClusterChain(entry.cluster, uint64(entry.Size))
}

// ExtractFile 提取文件到本地路径
func (fs *ExFATFileSystem) ExtractFile(srcPath, destPath string) error {
	data, err := fs.ReadFile(srcPath)
	if err != nil {
		return err
	}

	// 确保目标目录存在
	destDir := filepath.Dir(destPath)
	err = os.MkdirAll(destDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create destination directory: %v", err)
	}

	// 写入文件
	err = os.WriteFile(destPath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write file: %v", err)
	}

	return nil
}

// extractAllRecursive 递归提取目录内容的内部实现
func (fs *ExFATFileSystem) ExtractAllRecursive(srcPath, destPath string) error {
	// 获取当前目录的内容
	entries, err := fs.ListDir(srcPath)
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
			err := fs.extractAllRecursive(srcFullPath, destFullPath)
			if err != nil {
				// 这可能是空目录或无效簇号的目录，这是正常的
				fmt.Printf("Warning: Directory %s is empty or inaccessible: %v\n", entry.Name, err)
				// 但目录结构已经创建，所以继续处理其他项目
			}
		} else {
			// 处理文件
			if err := fs.ExtractFile(srcFullPath, destFullPath); err != nil {
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

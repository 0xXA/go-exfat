package exfat

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// OpenVHDFile 打开一个 VHD 文件
func OpenVHDFile(path string) (*VHDFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %v", err)
	}

	// 读取 VHD 头部（在文件末尾）
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to get file info: %v", err)
	}

	// 先尝试检查文件是否是标准 VHD 格式
	header, err := tryReadVHDHeader(file, stat.Size())
	if err != nil {
		// 如果不是标准 VHD，尝试作为原始磁盘映像处理
		return tryOpenAsRawDisk(file, stat.Size())
	}

	vhd := &VHDFile{
		file:   file,
		header: header,
	}

	// 检查磁盘类型
	switch header.DiskType {
	case FixedDisk: // 固定磁盘
		vhd.isDynamic = false
	case DynamicDisk: // 动态磁盘
		vhd.isDynamic = true
		if err := vhd.readDynamicHeader(); err != nil {
			file.Close()
			return nil, err
		}
	default:
		file.Close()
		return nil, fmt.Errorf("unsupported disk type: %d", header.DiskType)
	}

	return vhd, nil
}

// readVHDHeaderAt 在指定偏移读取 VHD 头部
func readVHDHeaderAt(file *os.File, offset int64) (*VHDHeader, error) {
	_, err := file.Seek(offset, io.SeekStart)
	if err != nil {
		return nil, err
	}

	header := &VHDHeader{}
	if err := binary.Read(file, binary.BigEndian, header); err != nil {
		return nil, err
	}
	if string(header.Cookie[:]) == "conectix" {
		return header, nil
	}

	return nil, fmt.Errorf("invalid VHD header")
}

// tryReadVHDHeader 尝试从多个位置读取 VHD 头部
func tryReadVHDHeader(file *os.File, fileSize int64) (*VHDHeader, error) {
	// 尝试从文件末尾读取 VHD 头部（标准位置）
	if header, err := readVHDHeaderAt(file, fileSize-SectorSize); err == nil {
		return header, nil
	}

	// 尝试从文件开头读取（某些工具创建的 VHD 可能把头部放在开头）
	if header, err := readVHDHeaderAt(file, 0); err == nil {
		return header, nil
	}

	return nil, fmt.Errorf("no valid VHD header found")
}

// tryOpenAsRawDisk 尝试作为原始磁盘映像打开
func tryOpenAsRawDisk(file *os.File, fileSize int64) (*VHDFile, error) {
	// 读取前 512 字节检查是否是 exFAT
	bootSector := make([]byte, SectorSize)
	if _, err := file.ReadAt(bootSector, 0); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to read boot sector: %v", err)
	}

	// 检查 exFAT 签名
	if isExFATBootSector(bootSector) {
		// 这是一个原始的 exFAT 磁盘映像，创建伪 VHD 头部
		return createPseudoVHD(file, fileSize), nil
	}

	file.Close()
	return nil, fmt.Errorf("invalid file format: not a standard VHD file or exFAT disk image")
}

// isExFATBootSector 检查引导扇区是否为 exFAT
func isExFATBootSector(data []byte) bool {
	return len(data) >= 11 && string(data[3:11]) == "EXFAT   "
}

// createPseudoVHD 为原始磁盘映像创建伪 VHD 结构
func createPseudoVHD(file *os.File, fileSize int64) *VHDFile {
	// 创建伪 VHD 头部用于原始磁盘映像
	header := &VHDHeader{
		DiskType:    FixedDisk, // 固定磁盘
		CurrentSize: uint64(fileSize),
	}
	copy(header.Cookie[:], "rawdisk") // 标记为原始磁盘

	return &VHDFile{
		file:      file,
		header:    header,
		isDynamic: false,
	}
}

// readDynamicHeader 读取动态磁盘头部
func (v *VHDFile) readDynamicHeader() error {
	// 定位到动态头部
	_, err := v.file.Seek(int64(v.header.DataOffset), io.SeekStart)
	if err != nil {
		return fmt.Errorf("failed to seek dynamic header: %v", err)
	}

	v.dynamicHeader = &VHDDynamicHeader{}
	err = binary.Read(v.file, binary.BigEndian, v.dynamicHeader)
	if err != nil {
		return fmt.Errorf("failed to read dynamic header: %v", err)
	}

	// 验证动态头部签名
	if string(v.dynamicHeader.Cookie[:]) != "cxsparse" {
		return fmt.Errorf("invalid dynamic disk header")
	}

	v.blockSize = v.dynamicHeader.BlockSize

	// 读取 BAT 表
	_, err = v.file.Seek(int64(v.dynamicHeader.TableOffset), io.SeekStart)
	if err != nil {
		return fmt.Errorf("failed to seek BAT table: %v", err)
	}

	v.bat = make([]uint32, v.dynamicHeader.MaxTableEntries)
	err = binary.Read(v.file, binary.BigEndian, v.bat)
	if err != nil {
		return fmt.Errorf("failed to read BAT table: %v", err)
	}

	return nil
}

// ReadAt 从指定偏移读取数据
func (v *VHDFile) ReadAt(buf []byte, offset int64) (int, error) {
	if !v.isDynamic {
		// 固定磁盘，直接读取
		return v.file.ReadAt(buf, offset)
	}

	// 动态磁盘，需要通过 BAT 表查找
	bytesRead := 0
	for len(buf) > 0 {
		blockIndex := uint32(offset / int64(v.blockSize))
		blockOffset := offset % int64(v.blockSize)

		if blockIndex >= uint32(len(v.bat)) {
			return bytesRead, io.EOF
		}

		// 计算本次读取的字节数
		var toRead int
		remaining := int(int64(v.blockSize) - blockOffset)
		if len(buf) < remaining {
			toRead = len(buf)
		} else {
			toRead = remaining
		}

		// 检查块是否分配
		if v.bat[blockIndex] == BlockUnallocated {
			for i := 0; i < toRead; i++ {
				buf[i] = 0
			}
		} else {
			// 计算块在文件中的实际偏移
			sectorOffset := int64(v.bat[blockIndex]) * SectorSize
			_, err := v.file.ReadAt(buf[:toRead], sectorOffset+blockOffset)
			if err != nil && err != io.EOF {
				return bytesRead, err
			}
		}

		buf = buf[toRead:]
		offset += int64(toRead)
		bytesRead += toRead
	}

	return bytesRead, nil
}

// Size 返回磁盘大小
func (v *VHDFile) Size() int64 {
	return int64(v.header.CurrentSize)
}

// Close 关闭 VHD 文件
func (v *VHDFile) Close() error {
	return v.file.Close()
}

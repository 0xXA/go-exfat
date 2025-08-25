package exfat

import (
	"io"
	"os"
)

// exFAT 目录条目类型
const (
	EntryTypeEndOfDirectory   = 0x00
	EntryTypeFile             = 0x85
	EntryTypeVolumeLabel      = 0x83
	EntryTypeAllocationBitmap = 0x81
	EntryTypeUpcaseTable      = 0x82
	EntryTypeFileInfo         = 0xC0
	EntryTypeFileName         = 0xC1
)

// 特殊簇值
const (
	EndOfClusterChain = 0xFFFFFFFF
	BadCluster        = 0xFFFFFFF7
	ReservedCluster   = 0xFFFFFFF8
)

// ExFATBootSector exFAT 引导扇区结构
type ExFATBootSector struct {
	JmpBoot                [3]byte   // 跳转指令
	FileSystemName         [8]byte   // "EXFAT   "
	Reserved1              [53]byte  // 保留
	PartitionOffset        uint64    // 分区偏移
	VolumeLength           uint64    // 卷长度
	FatOffset              uint32    // FAT 偏移
	FatLength              uint32    // FAT 长度
	ClusterHeapOffset      uint32    // 簇堆偏移
	ClusterCount           uint32    // 簇数量
	FirstClusterOfRootDir  uint32    // 根目录第一个簇
	VolumeSerialNumber     uint32    // 卷序列号
	FileSystemRevision     uint16    // 文件系统版本
	VolumeFlags            uint16    // 卷标志
	BytesPerSectorShift    uint8     // 每扇区字节数的位移
	SectorsPerClusterShift uint8     // 每簇扇区数的位移
	NumberOfFats           uint8     // FAT 数量
	DriveSelect            uint8     // 驱动器选择
	PercentInUse           uint8     // 使用百分比
	Reserved2              [7]byte   // 保留
	BootCode               [390]byte // 引导代码
	BootSignature          uint16    // 引导签名 (0xAA55)
}

// ExFATFileEntry exFAT 文件目录条目
type ExFATFileEntry struct {
	EntryType                 uint8
	SecondaryCount            uint8
	SetChecksum               uint16
	FileAttributes            uint16
	Reserved1                 uint16
	CreateTimestamp           uint32
	LastModifiedTimestamp     uint32
	LastAccessedTimestamp     uint32
	Create10msIncrement       uint8
	LastModified10msIncrement uint8
	CreateUtcOffset           uint8
	LastModifiedUtcOffset     uint8
	LastAccessedUtcOffset     uint8
	Reserved2                 [7]byte
}

// ExFATFileInfoEntry exFAT 文件信息条目
type ExFATFileInfoEntry struct {
	EntryType             uint8
	GeneralSecondaryFlags uint8
	Reserved1             uint8
	NameLength            uint8
	NameHash              uint16
	Reserved2             uint16
	ValidDataLength       uint64
	Reserved3             uint32
	FirstCluster          uint32
	DataLength            uint64
}

// ExFATFileNameEntry exFAT 文件名条目
type ExFATFileNameEntry struct {
	EntryType             uint8
	GeneralSecondaryFlags uint8
	FileName              [30]byte // UTF-16LE 编码的文件名
}

// ExFATFileSystem 表示 exFAT 文件系统
type ExFATFileSystem struct {
	vhd               io.ReaderAt
	bootSector        *ExFATBootSector
	bytesPerSector    uint32
	sectorsPerCluster uint32
	bytesPerCluster   uint32
	fat               []uint32
	clusterHeapStart  uint64
	totalClusters     uint32
}

// VHD 文件类型和常量
const (
	BlockUnallocated = 0xFFFFFFFF
	SectorSize       = 512
	FixedDisk        = 2
	DynamicDisk      = 3
)

// VHDHeader VHD 文件头部结构
type VHDHeader struct {
	Cookie             [8]byte   // "conectix"
	Features           uint32    // 功能标志
	FileFormatVersion  uint32    // 文件格式版本
	DataOffset         uint64    // 数据偏移（动态磁盘）
	TimeStamp          uint32    // 时间戳
	CreatorApplication [4]byte   // 创建应用
	CreatorVersion     uint32    // 创建版本
	CreatorHostOS      uint32    // 创建者操作系统
	OriginalSize       uint64    // 原始大小
	CurrentSize        uint64    // 当前大小
	DiskGeometry       uint32    // 磁盘几何
	DiskType           uint32    // 磁盘类型
	Checksum           uint32    // 校验和
	UniqueID           [16]byte  // 唯一ID
	SavedState         byte      // 保存状态
	Reserved           [427]byte // 保留字段
}

// VHDDynamicHeader VHD 动态磁盘头部
type VHDDynamicHeader struct {
	Cookie            [8]byte     // "cxsparse"
	DataOffset        uint64      // 数据偏移
	TableOffset       uint64      // BAT 表偏移
	HeaderVersion     uint32      // 头部版本
	MaxTableEntries   uint32      // 最大表项数
	BlockSize         uint32      // 块大小
	Checksum          uint32      // 校验和
	ParentUniqueID    [16]byte    // 父磁盘ID
	ParentTimeStamp   uint32      // 父磁盘时间戳
	Reserved1         uint32      // 保留
	ParentUnicodeName [512]byte   // 父磁盘名称
	ParentLocators    [8]struct { // 父磁盘定位器
		PlatformCode       uint32
		PlatformDataSpace  uint32
		PlatformDataLength uint32
		Reserved           uint32
		PlatformDataOffset uint64
	}
	Reserved2 [256]byte // 保留
}

// VHDFile 表示一个 VHD 文件
type VHDFile struct {
	file          *os.File
	header        *VHDHeader
	dynamicHeader *VHDDynamicHeader
	bat           []uint32 // Block Allocation Table
	blockSize     uint32
	isDynamic     bool
}

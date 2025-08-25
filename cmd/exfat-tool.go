package main

import (
	"flag"
	"fmt"
	"github.com/JoinChang/go-exfat"
	"os"
	"strings"
)

var (
	vhdPath   string
	listDir   string
	extract   string
	outputDir string
)

func init() {
	flag.StringVar(&vhdPath, "vhd", "", "Path to the VHD file")
	flag.StringVar(&listDir, "list", "", "Directory path inside the exFAT filesystem to list (optional)")
	flag.StringVar(&extract, "extract", "", "Comma-separated list of files/directories to extract (optional)")
	flag.StringVar(&outputDir, "output", "output", "Destination folder for extracted files (default: ./output)")

	flag.Usage = func() {
		fmt.Println("Usage: exfat-tool -vhd <path_to_vhd> [options]")
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()

	if vhdPath == "" {
		flag.Usage()
		return
	}

	vhd, err := exfat.OpenVHD(vhdPath)
	if err != nil {
		fmt.Printf("Failed to open VHD file: %v\n", err)
		return
	}
	defer vhd.Close()

	// 列目录
	if listDir != "" {
		entries, err := vhd.ListDir(listDir)
		if err != nil {
			fmt.Printf("Failed to list directory: %v\n", err)
			return
		}
		fmt.Printf("%-17s %-5s %-10s %s\n", "Modify Time", "Type", "Size", "Name")
		for _, entry := range entries {
			entryModTime := entry.ModTime.Format("2006-01-02 15:04")
			entryType := "File"
			if entry.IsDir {
				entryType = "Dir"
			}
			entrySize := exfat.FormatFileSize(entry.Size)
			if entry.IsDir {
				entrySize = "-"
			}
			fmt.Printf("%-17s %-5s %-10s %s\n", entryModTime, entryType, entrySize, entry.Name)
		}
		return
	}

	// 解压文件或目录
	if extract != "" {
		if err := os.MkdirAll(outputDir, os.ModePerm); err != nil {
			fmt.Printf("Failed to create output directory: %v\n", err)
			return
		}

		paths := strings.Split(extract, ",")
		for _, p := range paths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if err := vhd.ExtractFile(p, outputDir); err != nil {
				fmt.Printf("Failed to extract %s: %v\n", p, err)
			}
		}
		fmt.Printf("Extracted %s to %s\n", extract, outputDir)
		return
	}
}

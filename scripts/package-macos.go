//go:build ignore

// Package the cross-compiled Mac Agent with executable Unix permissions.
package main

import (
	"archive/tar"
	"compress/gzip"
	"debug/macho"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: package-macos <directory> <archive.tar.gz>")
	}
	source, output := os.Args[1], os.Args[2]
	binary, err := macho.Open(filepath.Join(source, "codex-remote"))
	if err != nil {
		return err
	}
	valid := binary.Cpu == macho.CpuArm64 && binary.Type == macho.TypeExec
	binary.Close()
	if !valid {
		return fmt.Errorf("Agent is not a macOS ARM64 executable")
	}
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	defer file.Close()
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	err = filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(filepath.Join(filepath.Base(source), relative))
		header.Mode = 0644
		if info.IsDir() || info.Name() == "codex-remote" || filepath.Ext(path) == ".command" {
			header.Mode = 0755
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		_, err = io.Copy(tw, input)
		return err
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return file.Close()
}

package util

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"

	"howett.net/plist"
)

func Get_creation_time(path string) (string, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	creationTime := fileInfo.ModTime()
	return creationTime.Format("02.01.2006"), nil
}

func Get_filename_hash(zipPath string) string {
	h := sha256.New()
	h.Write([]byte(zipPath))
	bs := hex.EncodeToString(h.Sum(nil))
	return bs
}

func Read_plist(f *zip.File) (map[string]any, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)

	var result map[string]any
	_, err = plist.Unmarshal(data, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

package util

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"io"
	"log"
	"monprojet/internal/model"
	"os"

	"github.com/sagernet/abx-go"
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

func Add_zip_path(PATHS map[string][]string, chemin, zipfile string) {
	PATHS[chemin] = append(PATHS[chemin], zipfile)
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

func Read_abx_files(f *zip.File) (*model.Packages, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	defer func() {
		if r := recover(); r != nil {
			log.Printf("ABX decode panic on file %s\n", f.Name)
		}
	}()

	reader, _ := abx.NewReader(bytes.NewReader(data))
	var decoder = xml.NewTokenDecoder(reader)
	var pkgs model.Packages

	if err := decoder.Decode(&pkgs); err != nil {
		return nil, err
	}

	return &pkgs, nil
}

func Build_json_results(path string, r model.Json_result) error {
	// écris les résultats au fur et à mesure du traitement dans le fichier json
	var list []model.Json_result

	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &list); err != nil {
			log.Println("JSON unmarshal error:", err)
		}
	}

	list = append(list, r)

	out, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		log.Println(err)
		return err
	}

	return os.WriteFile(path, out, 0644)
}

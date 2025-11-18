package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/sagernet/abx-go"
	"howett.net/plist"
)

type json_result struct {
	OS_type   string
	Version   string
	Hash      string
	Directory []string
	Packages  []package_info
}

type package_info struct {
	Name    string
	Version string
}

type Packages struct {
	XMLName xml.Name   `xml:"packages"`
	Package []PkgEntry `xml:"package"`
}

type PkgEntry struct {
	Name    string `xml:"name,attr"`
	Version string `xml:"version,attr"`
}

func main() {
	logFile := setup_logging()
	defer logFile.Close()

	dir := "D:\\"

	log.Println("Application started")
	var total_counter = listFiles(dir)

	log.Println("Number of Zip founded: ", total_counter)
	log.Println("Application Ended")
}

func LoadPackagesXml(f *zip.File) *Packages {

	rc, _ := f.Open()
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	reader, _ := abx.NewReader(bytes.NewReader(data))

	decoder := xml.NewTokenDecoder(reader)
	var pkgs Packages
	if err := decoder.Decode(&pkgs); err != nil {
		log.Fatalln(err)
	}

	return &pkgs
}

func setup_logging() *os.File {
	logFile, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}

	log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	return logFile
}

func listFiles(root string) int {
	var total_counter int = 0
	var extractions_counter int = 0
	var zip_path []string

	log.Println("Search Zip files")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			return nil
		}

		if strings.HasSuffix(strings.ToLower(d.Name()), ".zip") {
			total_counter++
			if !strings.Contains(strings.ToLower(path), "takeout") &&
				!strings.Contains(strings.ToLower(path), "icloud") &&
				!strings.Contains(strings.ToLower(path), "onedrive") &&
				!strings.Contains(strings.ToLower(path), "leapp") &&
				!strings.Contains(strings.ToLower(path), "axiom") {
				if !strings.Contains(strings.ToLower(path), "logical") {
					zip_path = append(zip_path, path)
				} else {
					extractions_counter++
				}
			}
		}
		return nil
	})
	log.Println("Number of Zip founded: ", total_counter)
	log.Println("Processing Zip files")

	processAllZips(zip_path, extractions_counter)

	if err != nil {
		log.Fatal(err)
	}
	return total_counter
}

func processAllZips(zipPaths []string, extractionsCounter int) {
	workerCount := runtime.NumCPU()
	jobs := make(chan string)
	var json_filename string = "results.json"
	var wg sync.WaitGroup

	var mu sync.Mutex

	for range workerCount {
		wg.Go(func() {
			for path := range jobs {

				dirs, infoResult, err := listDirsInZip(path)
				if err != nil || len(dirs) == 0 {
					continue
				}

				log.Printf("%s", path)

				if slices.Contains(dirs, "data/") {
					if len(infoResult.OS_type) == 0 {
						infoResult.OS_type = "Android"
					}

					mu.Lock()
					appendResultToJSONArray(json_filename, infoResult)
					extractionsCounter++
					mu.Unlock()

				} else if slices.Contains(dirs, "applications/") || slices.Contains(dirs, "private/") {
					if len(infoResult.OS_type) == 0 {
						infoResult.OS_type = "Apple"
					}

					mu.Lock()
					appendResultToJSONArray(json_filename, infoResult)
					extractionsCounter++
					mu.Unlock()
				} else {
					log.Println("TRIAGE!! Répertoires:", dirs)
				}
			}
		})
	}

	go func() {
		for _, path := range zipPaths {
			jobs <- path
		}
		close(jobs)
	}()

	wg.Wait()

	log.Println("Total extractions:", extractionsCounter)
}

func listDirsInZip(zipPath string) ([]string, json_result, error) {
	bs := get_filename_hash(zipPath)

	info_result := json_result{
		Hash: bs,
	}
	var packages_list []package_info

	r, _ := zip.OpenReader(zipPath)

	defer r.Close()

	dirSet := make(map[string]struct{})

	for _, f := range r.File {
		name := filepath.ToSlash(f.Name)

		pattern := `.*Dump/system/build\.prop$`
		re1, _ := regexp.Compile(pattern)

		pattern = `.*private/var/installd/Library/MobileInstallation/LastBuildInfo.plist$`
		re2, _ := regexp.Compile(pattern)

		pattern = `.*packages.xml$`
		re3, _ := regexp.Compile(pattern)

		if re1.MatchString(name) {
			result := get_android_details(f)

			info_result = json_result{
				OS_type: "Android",
				Version: result[0] + " " + result[1],
				Hash:    bs,
			}
		} else if re2.MatchString(name) {
			result := read_plist(f)

			productName, ok1 := result["ProductName"].(string)
			shortVersion, ok2 := result["ShortVersionString"].(string)

			if ok1 && ok2 {
				info_result = json_result{
					OS_type: productName,
					Version: shortVersion,
					Hash:    bs,
				}
			}
		} else if re3.MatchString(name) {
			pkgs := LoadPackagesXml(f)
			for _, p := range pkgs.Package {
				var app package_info
				app.Name = p.Name
				app.Version = p.Version
				packages_list = append(packages_list, app)
			}
		}

		parts := strings.Split(name, "/")
		if len(parts) > 2 {
			topDir := parts[1] + "/"
			dirSet[topDir] = struct{}{}
		}
	}

	var dirs []string
	for d := range dirSet {
		dirs = append(dirs, strings.ToLower(d))
	}
	sort.Strings(dirs)

	info_result.Directory = dirs
	info_result.Packages = packages_list
	return dirs, info_result, nil
}

func read_plist(f *zip.File) map[string]any {
	rc, err := f.Open()
	if err != nil {
		log.Fatal(err)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()

	var result map[string]any
	_, err = plist.Unmarshal(data, &result)
	if err != nil {
		log.Fatal(err)
	}
	return result
}

func get_android_details(f *zip.File) []string {
	prefixes := []string{
		"ro.build.version.release=",
		"ro.build.version.security_patch=",
	}

	rc, err := f.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer rc.Close()

	var result []string
	scanner := bufio.NewScanner(rc)

	for scanner.Scan() {
		line := scanner.Text()
		for _, prefix := range prefixes {
			if strings.HasPrefix(line, prefix) {
				result = append(result, line[len(prefix):])
				break
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
	return result
}

func get_filename_hash(zipPath string) string {
	h := sha256.New()
	h.Write([]byte(zipPath))
	bs := hex.EncodeToString(h.Sum(nil))
	return bs
}

func appendResultToJSONArray(path string, r json_result) error {
	var list []json_result

	data, _ := os.ReadFile(path)
	if len(data) > 0 {
		_ = json.Unmarshal(data, &list)
	}

	list = append(list, r)

	out, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, out, 0644)
}

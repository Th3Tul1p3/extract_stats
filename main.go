package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"database/sql"
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
	_ "modernc.org/sqlite"
)

type json_result struct {
	Manufacturer string
	Product_Type string
	Version      string
	Hash         string
	Directory    []string
	Packages     []string
}

type Packages struct {
	XMLName xml.Name   `xml:"packages"`
	Package []PkgEntry `xml:"package"`
}

type PkgEntry struct {
	Name    string `xml:"name,attr"`
	Version string `xml:"version,attr"`
}

type PackageXML struct {
	Name string `xml:"name,attr"`
}

type PackagesXML struct {
	Items []PackageXML `xml:"package"`
}

func main() {
	logFile := setup_logging()
	defer logFile.Close()

	var dir string

	if len(os.Args) > 1 {
		dir = os.Args[1]
	} else {
		dir = "S:\\"
	}

	log.Println("Application started in ", dir)
	var total_counter = list_zip_files(dir)

	log.Println("Number of Zip founded: ", total_counter)
	log.Println("Application Ended")
}

func LoadPackagesXml(f *zip.File) *Packages {
	var pkgs Packages

	rc, err := f.Open()
	if err != nil {
		log.Println(err)
		return &pkgs
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		log.Println(err)
		return &pkgs
	}
	reader, _ := abx.NewReader(bytes.NewReader(data))

	decoder := xml.NewTokenDecoder(reader)

	if err := decoder.Decode(&pkgs); err != nil {
		log.Println(err)
		return &pkgs
	}

	return &pkgs
}

func ExtractPackagesFromZipFile(f *zip.File) ([]PackageXML, error) {

	rc, err := f.Open()
	if err != nil {
		log.Println(err)
		return nil, err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	var root PackagesXML

	if err := xml.Unmarshal(data, &root); err != nil {
		log.Println(err)
		return nil, err
	}

	return root.Items, nil
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

func is_XML_file(f *zip.File) bool {

	rc, err := f.Open()
	if err != nil {
		log.Println(err)
	}

	defer rc.Close()
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return false
	}

	trimmed := bytes.TrimSpace(data)
	if !bytes.HasPrefix(trimmed, []byte("<?xml")) {
		return false
	}

	var tmp interface{}
	if err := xml.Unmarshal(data, &tmp); err != nil {
		return false
	}

	return true
}

func list_zip_files(root string) int {
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
			lower := strings.ToLower(path)

			if !strings.Contains(lower, "takeout") &&
				!strings.Contains(lower, "icloud") &&
				!strings.Contains(lower, "onedrive") &&
				!strings.Contains(lower, "leapp") &&
				!strings.Contains(lower, "axiom") {

				if !strings.Contains(lower, "logical") {
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

	process_all_zip(zip_path, extractions_counter)

	if err != nil {
		log.Fatal(err)
	}
	return total_counter
}

func process_all_zip(zipPaths []string, extractionsCounter int) {
	workerCount := runtime.NumCPU()
	jobs := make(chan string)
	var json_filename string = "results.json"
	var wg sync.WaitGroup

	var mu sync.Mutex

	for range workerCount {
		wg.Go(func() {
			for path := range jobs {
				log.Println(path)
				dirs, infoResult, err := extract_infos_zip(path)
				if err != nil || len(dirs) == 0 {
					continue
				}

				log.Printf("%s", path)

				if slices.Contains(dirs, "data/") {
					if len(infoResult.Manufacturer) == 0 {
						infoResult.Manufacturer = "Android"
					}

					mu.Lock()
					build_json_results(json_filename, infoResult)
					extractionsCounter++
					mu.Unlock()

				} else if slices.Contains(dirs, "applications/") || slices.Contains(dirs, "private/") {
					if len(infoResult.Manufacturer) == 0 {
						infoResult.Manufacturer = "Apple"
					}

					mu.Lock()
					build_json_results(json_filename, infoResult)
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

func extract_infos_zip(zipPath string) ([]string, json_result, error) {
	bs := get_filename_hash(zipPath)

	info_result := json_result{
		Hash: bs,
	}
	var packages_list []string

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		log.Println(err)
		return nil, info_result, err
	}

	defer r.Close()

	dirSet := make(map[string]struct{})

	pattern := `.*Dump/system/build\.prop$`
	build_prop, _ := regexp.Compile(pattern)

	pattern = `.*private/var/installd/Library/MobileInstallation/LastBuildInfo\.plist$`
	lastbuildinfo, _ := regexp.Compile(pattern)

	pattern = `.*/data/system/packages\.xml$`
	packages, _ := regexp.Compile(pattern)

	pattern = `.*private/var/mobile/Library/FrontBoard/applicationState\.db$`
	application_state, _ := regexp.Compile(pattern)

	pattern = `.*private/var/containers/Data/System/.*/Library/activation_records/activation_record\.plist$`
	activation_record, _ := regexp.Compile(pattern)

	for _, f := range r.File {
		name := filepath.ToSlash(f.Name)
		if build_prop.MatchString(name) {
			result := get_android_details(f)
			if len(result) == 4 {
				info_result.Manufacturer = result[0]
				info_result.Version = result[2] + " " + result[3]
				info_result.Product_Type = result[1]
			} else {
				log.Println(result)
			}
		} else if lastbuildinfo.MatchString(name) {
			result := read_plist(f)
			if result != nil {
				productName, ok1 := result["ProductName"].(string)
				shortVersion, ok2 := result["ShortVersionString"].(string)

				if ok1 && ok2 {
					info_result.Manufacturer = productName
					info_result.Version = shortVersion
				}
			}
		} else if packages.MatchString(name) {
			if !is_XML_file(f) {
				pkgs := LoadPackagesXml(f)
				for _, p := range pkgs.Package {
					packages_list = append(packages_list, p.Name)
				}
			} else {
				log.Println(zipPath)
			}
		} else if application_state.MatchString(name) {
			var rows *sql.Rows = extract_apps_in_sqlite(f)
			if rows == nil {
				log.Println(zipPath)
			} else {
				for rows.Next() {
					var t string
					rows.Scan(&t)
					packages_list = append(packages_list, t)
				}
				rows.Close()
			}
		} else if activation_record.MatchString(name) {
			result := read_plist(f)
			if result != nil {
				var test = result["AccountToken"].([]byte)
				var jsonMap map[string]interface{}
				_, _ = plist.Unmarshal(test, &jsonMap)
				info_result.Product_Type = jsonMap["ProductType"].(string)
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
		log.Println(err)
		return nil
	}
	data, _ := io.ReadAll(rc)
	rc.Close()

	var result map[string]any
	_, err = plist.Unmarshal(data, &result)
	if err != nil {
		log.Println(err)
		return nil
	}
	return result
}

func get_android_details(f *zip.File) []string {
	prefixes := []string{
		"ro.build.version.release=",
		"ro.build.version.security_patch=",
		"ro.product.system.brand=",
		"ro.product.system.model=",
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

func build_json_results(path string, r json_result) error {
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

func extract_apps_in_sqlite(f *zip.File) *sql.Rows {
	rc, _ := f.Open()

	data, err := io.ReadAll(rc)
	if err != nil {
		log.Println(err)
		return nil
	}
	rc.Close()

	tmp, err := os.CreateTemp("", "sqlite-*.db")
	if err != nil {
		log.Println(err)
		return nil
	}
	defer tmp.Close()

	if _, err := tmp.Write(data); err != nil {
		log.Println(err)
		return nil
	}

	db, _ := sql.Open("sqlite", tmp.Name())
	defer db.Close()

	rows, _ := db.Query("select application_identifier from application_identifier_tab")

	return rows
}

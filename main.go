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
	"syscall"
	"time"
	"unsafe"

	"github.com/sagernet/abx-go"
	"howett.net/plist"
	_ "modernc.org/sqlite"
)

type json_result struct {
	Manufacturer    string   `json:"manufacturer"`
	Logical_path    string   `json:"logical_path"`
	Date_extraction string   `json:"date_extraction"`
	Product_Type    string   `json:"product_type"`
	Version         string   `json:"version"`
	Hash            string   `json:"hash"`
	Directory       []string `json:"directory"`
	Packages        []string `json:"packages"`
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
	list_zip_files(dir)

	log.Println("Application Ended")
}

func OpenDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite", "zip.sqlite")
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS paths (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            value TEXT NOT NULL
        );
    `)
	if err != nil {
		return nil, err
	}

	return db, nil
}

func InsertStrings(db *sql.DB, values []string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	_, err = db.Exec("DELETE FROM paths")
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare("INSERT INTO paths(value) VALUES (?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, v := range values {
		_, err := stmt.Exec(v)
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

func GetAllValues(db *sql.DB) ([]string, error) {
	rows, err := db.Query("SELECT value FROM paths")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		result = append(result, v)
	}

	return result, rows.Err()
}

func Read_abx_files(f *zip.File) (*Packages, error) {
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
	var pkgs Packages

	if err := decoder.Decode(&pkgs); err != nil {
		return nil, err
	}

	return &pkgs, nil
}

func ExtractPackagesFromZipFile(f *zip.File) ([]PackageXML, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	var root PackagesXML

	if err := xml.Unmarshal(data, &root); err != nil {
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
		return false
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return false
	}

	trimmed := bytes.TrimSpace(data)
	return bytes.HasPrefix(trimmed, []byte("<?xml"))
}

func list_zip_files(root string) {
	var total_counter int = 0
	var extractions_counter int = 0
	var zip_path []string

	var db, err = OpenDB()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	values, err := GetAllValues(db)
	if err != nil {
		log.Fatal(err)
	}

	if len(values) > 0 {
		log.Println("taking zip files from sqlite")
		zip_path = values
	} else {
		log.Println("Search Zip files")
		err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
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

					if !strings.Contains(lower, "logical") && !strings.Contains(lower, "wiko") {
						zip_path = append(zip_path, path)
					} else {
						extractions_counter++
					}
				}
			}
			return nil
		})

		if err != nil {
			log.Println(err)
		}

		log.Println("Number of Zip founded: ", total_counter)
		log.Println("Écriture terminée dans zip.sqlite")
		if err := InsertStrings(db, zip_path); err != nil {
			log.Fatal(err)
		}
	}

	log.Println("Processing Zip files")
	process_all_zip(zip_path, extractions_counter)
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
				_, err := os.Stat(path)
				if err != nil {
					log.Println("Ce zip n'existe plus: ", path)
					continue
				}

				dirs, infoResult, err := extract_infos_zip(path)
				if err != nil || len(dirs) == 0 {
					continue
				}

				t, err := get_creation_time(path)
				if err != nil {
					panic(err)
				}

				infoResult.Date_extraction = t

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
					log.Printf("%s", path)
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

	log.Println("Total extractions parsed:", extractionsCounter)
}

func extract_infos_zip(zipPath string) ([]string, json_result, error) {
	bs := get_filename_hash(zipPath)

	info_result := json_result{
		Hash:         bs,
		Logical_path: zipPath,
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
			} else if len(result) == 2 {
				info_result.Version = result[0] + " " + result[1]
			} else {
				log.Println(result)
			}
		} else if lastbuildinfo.MatchString(name) {
			result, err := read_plist(f)
			if err == nil {
				productName, ok1 := result["ProductName"].(string)
				shortVersion, ok2 := result["ShortVersionString"].(string)

				if ok1 && ok2 {
					info_result.Manufacturer = productName
					info_result.Version = shortVersion
				}
			} else {
				log.Println(err)
			}
		} else if packages.MatchString(name) {
			if !is_XML_file(f) {
				pkgs, err := Read_abx_files(f)
				if pkgs != nil {
					for _, p := range pkgs.Package {
						packages_list = append(packages_list, p.Name)
					}
				} else {
					log.Println(err)
				}
			} else {
				var _, err = ExtractPackagesFromZipFile(f)
				if err != nil {
					log.Println("Error on: ", zipPath)
				}
			}
		} else if application_state.MatchString(name) {
			var rows, err = extract_apps_in_sqlite(f)
			if err != nil || rows == nil {
				log.Println(zipPath, err)
			} else {
				for rows.Next() {
					var t string
					rows.Scan(&t)
					packages_list = append(packages_list, t)
				}
				rows.Close()
			}
		} else if activation_record.MatchString(name) {
			result, err := read_plist(f)
			if err == nil {
				var test = result["AccountToken"].([]byte)
				var jsonMap map[string]any
				_, _ = plist.Unmarshal(test, &jsonMap)
				info_result.Product_Type = jsonMap["ProductType"].(string)
			} else {
				log.Println(err)
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

func read_plist(f *zip.File) (map[string]any, error) {
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

func get_android_details(f *zip.File) []string {
	rc, err := f.Open()
	if err != nil {
		log.Println(err)
	}
	defer rc.Close()
	prefixes := []string{
		"ro.build.version.release=",
		"ro.build.version.security_patch=",
		"ro.product.system.brand=",
		"ro.product.system.model=",
	}

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
		log.Println(err)
		return nil
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

func extract_apps_in_sqlite(f *zip.File) (*sql.Rows, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp("", "sqlite-*.db")
	if err != nil {
		return nil, err
	}
	defer tmp.Close()

	if _, err := tmp.Write(data); err != nil {
		return nil, err
	}

	db, _ := sql.Open("sqlite", tmp.Name())

	rows, _ := db.Query("select application_identifier from application_identifier_tab")
	db.Close()
	return rows, nil
}

func get_creation_time(path string) (string, error) {
	var data syscall.Win32FileAttributeData
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}

	err = syscall.GetFileAttributesEx(p, syscall.GetFileExInfoStandard, (*byte)(unsafe.Pointer(&data)))
	if err != nil {
		return "", err
	}

	t := time.Unix(0, data.CreationTime.Nanoseconds())
	return t.Format("02.01.2006"), nil
}

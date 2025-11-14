package main

import (
    "log"
    "os"
    "path/filepath"
	"io"
	"strings"
	"archive/zip"
	"regexp"
	"bufio"
	"sort"
	"crypto/sha256"
	"slices"
	"howett.net/plist"
	"encoding/json"
	"encoding/hex"
)

type json_result struct {
    OS_type   string
    Version string 
	Hash string
}

func main() {
	logFile, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
    if err != nil {
        log.Fatalf("Failed to open log file: %v", err)
    }

	defer logFile.Close()
	log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Application started")

    dir := "D:\\"

    var total_counter, extractions_counter = listFiles(dir)

	log.Println("Number of Zip founded: ", total_counter)
	log.Println("Number of Extractions founded: ", extractions_counter)
	log.Println("Application Ended")
}

func listFiles(root string) (int, int) {
	var total_counter int = 0
	var extractions_counter int = 0

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			log.Printf("Impossible de lire %s : %v\n", path, err)
			return nil
		}

		if d.IsDir() {
			return nil
		}

		if strings.HasSuffix(strings.ToLower(d.Name()), ".zip") {
			total_counter++
			dirs, info_result, err := listDirsInZip(path)
			if len(dirs) == 0 || err != nil {
				return nil 
			}

			log.Printf("%s", path)

			info_result_json, _ := json.Marshal(info_result)

			if slices.Contains(dirs, "data/") {
				log.Println("LIKELY ANDROID DEVICE, Répertoires:", dirs)
				log.Println(string(info_result_json))
				extractions_counter++
			} else if slices.Contains(dirs, "applications/") || slices.Contains(dirs, "private/"){
				log.Println("LIKELY APPLE DEVICE, Répertoires:", dirs)
				log.Println(string(info_result_json))
				extractions_counter++
			} else {
				log.Println("TRIAGE!! Répertoires:", dirs)
			}
		}

		return nil
	})

	if err != nil {
		log.Fatal(err)
	}
	return total_counter, extractions_counter
}

func listDirsInZip(zipPath string) ([]string, json_result, error) {
	h := sha256.New()
	h.Write([]byte(zipPath))
	bs := hex.EncodeToString(h.Sum(nil))

	info_result := json_result{
		Hash: bs,
	}

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, info_result, err
	}
	defer r.Close()

	dirSet := make(map[string]struct{})

	for _, f := range r.File {
		name := filepath.ToSlash(f.Name)

		pattern := `.*Dump/system/build\.prop$`
		re1, err1 := regexp.Compile(pattern)

		pattern = `.*private/var/installd/Library/MobileInstallation/LastBuildInfo.plist$`
		re2, err2 := regexp.Compile(pattern)
		if err2 != nil || err1 != nil {
			log.Fatal(err)
		}

		if re1.MatchString(name) {
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

			info_result = json_result{
				OS_type:   "Android",
				Version: result[0] + " " + result[1],
				Hash: bs,
			}
		} else if re2.MatchString(name){
			rc, err := f.Open()
			if err != nil {
				log.Fatal(err)
			}
			data, _ := io.ReadAll(rc)
			rc.Close()

			var result map[string]any
			_, err = plist.Unmarshal(data, &result)
			if err != nil {
				return nil, info_result, err
			}

			productName, ok1 := result["ProductName"].(string)
			shortVersion, ok2 := result["ShortVersionString"].(string)

			if ok1 && ok2{
				info_result = json_result{
					OS_type: productName,
					Version: shortVersion,
					Hash: bs,
				}
			} 
		}

		parts := strings.Split(name, "/")
		//log.Printf(" %s %d %s \n", parts, len(parts), parts[1])
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
	return dirs, info_result, nil
}
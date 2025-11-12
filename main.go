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
)

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

    counter := listFiles(dir)

	println("Number of Zip founded: ", counter)

	log.Println("Application Ended")
}

func listFiles(root string) int {
	var counter int = 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			log.Printf("Impossible de lire %s : %v\n", path, err)
			return nil
		}

		if d.IsDir() {
			return nil
		}

		if strings.HasSuffix(strings.ToLower(d.Name()), ".zip") {
			log.Printf(path)

			dirs, err := listDirsInZip(path)
			if err != nil {
				log.Fatal(err)
			}

			log.Println("Répertoires:", dirs)
			counter++
		}

		return nil
	})

	if err != nil {
		log.Fatal(err)
	}
	return counter
}

func listDirsInZip(zipPath string) ([]string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	dirSet := make(map[string]struct{})

	for _, f := range r.File {
		name := filepath.ToSlash(f.Name)

		pattern := `.*Dump/system/build\.prop$`
		re, err := regexp.Compile(pattern)
		if err != nil {
			log.Fatal(err)
		}

		if re.MatchString(name) {
			prefixes := []string{
				"ro.system.build.version.release=",
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
						result = append(result, line)
						break 
					}
				}
			}

			if err := scanner.Err(); err != nil {
				log.Fatal(err)
			}
			log.Println(result)
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
		dirs = append(dirs, d)
	}

	return dirs, nil
}
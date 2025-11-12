package main

import (
    "fmt"
    "log"
    "os"
    "path/filepath"
	"io"
	"strings"
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

    dir := "D:"

    files := listFiles(dir)

    for _, v := range files {
       fmt.Println(v)
    }
	log.Println("Application Ended")
}

func listFiles(root string) []string {
	var zipFiles []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			log.Printf("Impossible de lire %s : %v\n", path, err)
			return nil
		}

		if d.IsDir() {
			return nil
		}

		if strings.HasSuffix(strings.ToLower(d.Name()), ".zip") {
			zipFiles = append(zipFiles, path)
		}

		return nil
	})


	if err != nil {
		log.Fatal(err)
	}

	return zipFiles
}
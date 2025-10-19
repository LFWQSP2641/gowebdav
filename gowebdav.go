// Source code: https://doc.xuwenliang.com/docs/go/1814

package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"golang.org/x/net/context"
	"golang.org/x/net/webdav"
)

var (
	flagRootDir   = flag.String("dir", "", "webdav root dir")
	flagHttpAddr  = flag.String("http", ":80", "http or https address")
	flagHttpsMode = flag.Bool("https-mode", false, "use https mode")
	flagCertFile  = flag.String("https-cert-file", "cert.pem", "https cert file")
	flagKeyFile   = flag.String("https-key-file", "key.pem", "https key file")
	flagUserName  = flag.String("user", "", "user name")
	flagPassword  = flag.String("password", "", "user password")
	flagReadonly  = flag.Bool("read-only", false, "read only")
)

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of WebDAV Server\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nReport bugs to <chaishushan@gmail.com>.\n")
	}
}

type SkipBrokenLink struct {
	webdav.Dir
}

func (d SkipBrokenLink) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	fileinfo, err := d.Dir.Stat(ctx, name)
	if err != nil && os.IsNotExist(err) {
		// Return the original error, not filepath.SkipDir
		// filepath.SkipDir can cause issues with WebDAV MOVE operations
		return nil, os.ErrNotExist
	}
	return fileinfo, err
}

func (d SkipBrokenLink) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	file, err := d.Dir.OpenFile(ctx, name, flag, perm)
	if err != nil && os.IsNotExist(err) {
		return nil, os.ErrNotExist
	}
	return file, err
}

func main() {
	flag.Parse()
	fs := &webdav.Handler{
		FileSystem: SkipBrokenLink{webdav.Dir(*flagRootDir)},
		LockSystem: webdav.NewMemLS(),
	}
	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if *flagUserName != "" && *flagPassword != "" {
			username, password, ok := req.BasicAuth()
			if !ok {
				w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if username != *flagUserName || password != *flagPassword {
				http.Error(w, "WebDAV: need authorized!", http.StatusUnauthorized)
				return
			}
		}
		// Only show directory listing for browser GET requests, not WebDAV clients
		// WebDAV clients typically send Depth header or User-Agent with "WebDAV" in it
		if req.Method == "GET" && req.Header.Get("Depth") == "" && req.Header.Get("Translate") == "" && handleDirList(fs.FileSystem, w, req) {
			return
		}
		if *flagReadonly {
			switch req.Method {
			case "PUT", "DELETE", "PROPPATCH", "MKCOL", "COPY", "MOVE":
				http.Error(w, "WebDAV: Read Only!!!", http.StatusForbidden)
				return
			}
		}
		fs.ServeHTTP(w, req)
	})
	if *flagHttpsMode {
		http.ListenAndServeTLS(*flagHttpAddr, *flagCertFile, *flagKeyFile, nil)
	} else {
		http.ListenAndServe(*flagHttpAddr, nil)
	}
}

func handleDirList(fs webdav.FileSystem, w http.ResponseWriter, req *http.Request) bool {
	ctx := context.Background()

	// First check if the path exists and is a directory without opening it
	fi, err := fs.Stat(ctx, req.URL.Path)
	if err != nil {
		return false
	}

	// Only handle directory listing, not files
	if !fi.IsDir() {
		return false
	}

	// Ensure the path ends with / for directories
	if !strings.HasSuffix(req.URL.Path, "/") {
		http.Redirect(w, req, req.URL.Path+"/", 302)
		return true
	}

	// Open the directory
	f, err := fs.OpenFile(ctx, req.URL.Path, os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	defer f.Close()

	// Read directory contents
	dirs, err := f.Readdir(-1)
	if err != nil {
		http.Error(w, "Error reading directory", http.StatusInternalServerError)
		return true // Return true because we've already written the response
	}

	// Send HTML directory listing
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "<pre>\n")
	for _, d := range dirs {
		link := d.Name()
		if d.IsDir() {
			link += "/"
		}
		name := link
		if (d.Mode() & os.ModeSymlink) == os.ModeSymlink {
			name += "@"
		}
		fmt.Fprintf(w, "<a href=\"%s\">%s</a>\n", link, name)
	}
	fmt.Fprintf(w, "</pre>\n")
	return true
}

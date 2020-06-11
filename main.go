/*
Generate HTML directory listings at .directory.html, recursively within basedir and its subdirectories
basedirectory must be absolute
// relative paths require that the uri for a directory welcome page ends in / for the browser to process them properly
// so what Apache does is redirect you to the uri ending in / if you are looking for a directory
// This program makes all links relative to basedirectory (with preceding /)
*/
package main

import (
	"bufio"
	"flag"
	"fmt"
	"html/template"
	"htmldir/recursivedirwatch"
	"io/ioutil"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type templatedir struct {
	Dirname string
	Files   []templatefile
}

type templatefile struct {
	Filetype     string
	Name         string
	Link         string
	Size         string
	LastModified string
}

const outputfilename = ".directory.html"
const kb = 1024
const mb = 1024 * 1024
const gb = 1024 * 1024 * 1024
const tb = 1024 * 1024 * 1024 * 1024

func main() {
	// Process arguments
	// var regenall *bool = flag.Bool("all", false, "Regenerate all files before setting watches")
	// var exit *bool = flag.Bool("exit", false, "Exit after regenerating all files")
	var regenall bool = *(flag.Bool("all", false, "Regenerate all files before setting watches"))
	var exit bool = *(flag.Bool("exit", false, "Exit after regenerating all files"))
	flag.Parse()
	if len(flag.Args()) != 1 {
		fmt.Println("Usage:", os.Args[0], "[-all] [-exit] basedirectory")
		return
	}
	var basedir string = flag.Args()[0]

	// Remove trailing slash from dirname
	if len(basedir) > 1 && basedir[len(basedir)-1] == '/' {
		basedir = basedir[:len(basedir)-1]
	}
	fmt.Println("Directory:", basedir)
	//-------------------------------------------------------------------------

	var err error // = nil // This would get compiled out right becauase already inititalised right?
	//var direvent recursivedirwatch.DirEvent

	var tmpl *template.Template = template.Must(template.New("dirtemplate.html").ParseFiles("dirtemplate.html"))
	if regenall && exit {
		err = filepath.Walk(basedir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				makeHTML(path, basedir, tmpl)
			}
			return nil
		})
		if err != nil {
			fmt.Println(err)
		}
	} else {
		ch := make(chan recursivedirwatch.Event, 5) // Uber's go style guide says don't use buffered channels but I don't understand why
		go recursivedirwatch.Watch(basedir, ch, regenall)
		for event := range ch {
			//recursivedirwatch.PrintEvent(event)
			if event.Name == nil || *event.Name != outputfilename {
				fmt.Println("Making HTML for", event.Dirpath)
				err = makeHTML(event.Dirpath, basedir, tmpl)
				if err != nil {
					fmt.Println(err)
				}
			}
		}
	}
}

func makeHTML(dirpath string, basedir string, tmpl *template.Template) error {
	relpath := getRelativePath(basedir, dirpath)
	dir, err := buildTemplateInputs(dirpath, relpath)
	if err != nil {
		return err
	}
	err = writeTemplate(dirpath+"/"+outputfilename, tmpl, dir)
	return err
}

func buildTemplateInputs(path string, relativepath string) (templatedir, error) {
	files, err := ioutil.ReadDir(path) // uses lstat
	if err != nil {
		return templatedir{}, err
	}
	templatedirv := templatedir{relativepath, make([]templatefile, 0, len(files))}
	
	if relativepath != "" {
		// add parent directory
		parentpath := parentdir(path)
		parent, err := os.Lstat(parentpath)
		if err != nil {
			return templatedir{}, err
			// what does templatedir{} actually return. Answer: all struct fields zero initialised
		}
		templatedirv.Files = append(templatedirv.Files, templatefile{"DIR", "../", "/" + parentdir(relativepath), "", parent.ModTime().Format("02-Jan-2006  15:04:05 MST")})
	}

	var filetype string
	var filesize string
	for _, file := range files {
		filename := file.Name()
		if file.Mode()&os.ModeSymlink == 0 && filename != outputfilename /*&& isReadable(relativepath, file)*/ {
			// actually maybe I do want to include symlinks
			if file.IsDir() {
				filetype = "DIR"
				filesize = ""
				filename += "/"
			} else {
				filetype = "FILE"
				filesize = filesizestr(file.Size())
			}
			var lastmodified string = file.ModTime().Format("02-Jan-2006  15:04:05 MST")
			var link string
			if relativepath == "" {
				link = "/" + filename
			} else {
				link = "/" + relativepath + "/" + filename
			}
			//fmt.Println(url.PathEscape(link)) // This encodes slashes... weird

			encodedLink, err := url.Parse(link)
			if err != nil {
				return templatedir{}, err
			}

			templatedirv.Files = append(templatedirv.Files, templatefile{filetype, filename, encodedLink.String(), filesize, lastmodified})
		}
	}
	return templatedirv, nil
}

// if basedir is prefix of dirpath, return dirpath - basedir.
// else return empty string
// better way?
func getRelativePath(basedir string, dirpath string) string {
	var relative []string = strings.Split(dirpath, basedir+"/")
	// Split() returns slices of original string ie not new string
	if len(relative) < 2 {
		return ""
	} else {
		return relative[1]
	}
}

// filesizenum is number of bytes
func filesizestr(filesizebytes int64) string {
	var filesize string
	// int64 fits in float64
	switch {
	case filesizebytes < kb:
		filesize = fmt.Sprintf("%d", filesizebytes)
	case filesizebytes < mb:
		filesize = fmt.Sprintf("%vKB", math.Ceil(float64(filesizebytes)/kb))
	case filesizebytes < gb:
		filesize = fmt.Sprintf("%vMB", math.Ceil(float64(filesizebytes)/mb))
	case filesizebytes < tb:
		filesize = fmt.Sprintf("%.1fGB", float64(filesizebytes)/gb)
	default:
		filesize = fmt.Sprintf("%.1fTB", float64(filesizebytes)/tb)
	}
	return filesize
	// test the sizes with big fake inputs
	// the numbers printed seem to be a big wrong eg 16MB when computer says 17.2
	// I think it is divide by 1024 vs divide by 1000
}

func writeTemplate(location string, tmpl *template.Template, dir templatedir) error {
	file, err := os.Create(location) // why didn't this cause inotify infinite loop when modidy was accepted
	if err != nil {
		return err
	}
	defer file.Close()
	filewriter := bufio.NewWriter(file)
	err = tmpl.Execute(filewriter, dir)
	if err != nil {
		return err
	}
	err = filewriter.Flush()
	return err
}

// todo?
func isReadable(dir string, file os.FileInfo) bool {
	// check that files are readable before adding
	// file.Mode()&os.ModePerm
	// will require checking if is owner and owner permissions,
	// but also checking all groups the user is in and if any of them have access
	// Linux has access() and euidaccess()
	return true
}

// stringUpToLast
// Return string slice up to and including last '/'' in directory string.
// If '/' never appears, return empty string
func parentdir(directory string) string {
	for i := len(directory) - 1; i > 0; i-- {
		if directory[i] == '/' {
			return directory[:i+1]
		}
	}
	return ""
}

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/h2non/bimg"

	"github.com/vjdw/chronoshot/db"

	"github.com/rjeczalik/notify"

	"github.com/rwcarlsen/goexif/exif"
)

// "github.com/nfnt/resize" replaced by "gopkg.in/h2non/bimg.v1"
// requires libvips to be installed

func rootHandler(w http.ResponseWriter, r *http.Request) {
	f, err := os.Open("./index.html")
	if err != nil {
		log.Fatal(err)
	}
	if _, err := io.Copy(w, f); err != nil {
		log.Fatal(err)
	}
}

func getImageCountHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	response := strconv.Itoa(db.GetLengthOfIndex())
	w.Header().Set("Content-Length", strconv.Itoa(len(response)))
	if _, err := w.Write([]byte(response)); err != nil {
		log.Println("unable to write response.")
	}
}

func getImageHandler(w http.ResponseWriter, r *http.Request) {

	id := r.URL.Query().Get("id")
	index, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		log.Fatal(err)
	}
	imgPath := db.GetKeyByIndex(index)

	f, err := os.Open(string(imgPath[:]))
	if err != nil {
		log.Fatal(err)
	}

	var buf bytes.Buffer
	_, err = buf.ReadFrom(f)
	if err != nil {
		log.Fatal(err)
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(buf.Bytes())))
	if _, err := w.Write(buf.Bytes()); err != nil {
		log.Println("unable to write image.")
	}
}

func getThumbnailHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	index, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
		//log.Fatal(err)
	}
	buf := db.GetImageByIndex(index)

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
	if _, err := w.Write(buf); err != nil {
		log.Println("unable to write image.")
	}
}

func updateDatabaseHandler(w http.ResponseWriter, r *http.Request) {

}

func watchDirectory() {
	// Make the channel buffered to ensure no event is dropped. Notify will drop
	// an event if the receiver is not able to keep up the sending pace.
	c := make(chan notify.EventInfo, 1)

	// Set up a watchpoint listening for events within a directory tree rooted
	// at current working directory. Dispatch remove events to c.
	if err := notify.Watch("./...", c, notify.Create|notify.Rename|notify.Remove); err != nil {
		log.Fatal(err)
	}
	defer notify.Stop(c)

	// Block until an event is received.
	for {
		ei := <-c
		//storeThumbnail(ei.Path, )
		log.Println("Got event:", ei)
	}
}

var concurrency = 8
var rateLimiter = make(chan bool, concurrency)

func processPhoto(path string, info os.FileInfo, err error) error {
	if err != nil {
		log.Print(err)
		return nil
	}

	rateLimiter <- true
	go func(string) {
		defer func() { <-rateLimiter }()
		lowerPath := strings.ToLower(path)
		if strings.HasSuffix(lowerPath, "jpg") || strings.HasSuffix(lowerPath, "jpeg") {

			var imageDbKey = []byte(path)
			if db.KeyExists(imageDbKey) {
				fmt.Printf("Already in database: %s\n", path)
				return
			}
			fmt.Println(path)
			datetime := getExifDateTime(path)
			storeThumbnail(imageDbKey, path, datetime)
		}
	}(path)

	return nil
}

func getExifDateTime(path string) time.Time {
	f, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}

	// Optionally register camera makenote data parsing - currently Nikon and
	// Canon are supported.
	//exif.RegisterParsers(mknote.All...)

	x, err := exif.Decode(f)
	if err != nil {
		//log.Fatal(err)
		return time.Unix(0, 0)
	}

	//camModel, _ := x.Get(exif.Model) // normally, don't ignore errors!
	//fmt.Println(camModel.StringVal())

	//focal, _ := x.Get(exif.FocalLength)
	//numer, denom, _ := focal.Rat2(0) // retrieve first (only) rat. value
	//fmt.Printf("%v/%v", numer, denom)

	// Two convenience functions exist for date/time taken and GPS coords:
	tm, _ := x.DateTime()
	fmt.Println("Taken: ", tm)

	lat, long, _ := x.LatLong()
	fmt.Println("lat, long: ", lat, ", ", long)

	return tm
}

func storeThumbnail(imageDbKey []byte, path string, datetime time.Time) error {
	// file, err := os.Open(path)
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// // decode jpeg into image.Image
	// img, err := jpeg.Decode(file)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// file.Close()

	// resize to width 200 using Lanczos resampling
	// and preserve aspect ratio
	//thumbnail := resize.Resize(200, 0, img, resize.Lanczos3)

	buffer, err := bimg.Read(path)
	if err != nil {
		log.Fatal(err)
	}

	//newImage, err := bimg.NewImage(buffer).Resize(800, 600)
	thumbnail, err := bimg.NewImage(buffer).Thumbnail(200)
	if err != nil {
		log.Fatal(err)
	}

	// size, err := bimg.NewImage(newImage).Size()
	// if size.Width == 400 && size.Height == 300 {
	// 	fmt.Println("The image size is valid")
	// }

	// write new image to file
	//buf := new(bytes.Buffer)
	//jpeg.Encode(buf, thumbnail, nil)
	//db.PutImage(imageDbKey, datetime, buf.Bytes())
	db.PutImage(imageDbKey, datetime, thumbnail)

	return nil
}

func main() {
	db.Init()

	//dir := "/home/vin/Desktop/scratch"
	dir := "/media/data/photos"

	if err := filepath.Walk(dir, processPhoto); err != nil {
		log.Fatal(err)
	}
	// Flush out final workers.
	for i := 0; i < cap(rateLimiter); i++ {
		rateLimiter <- true
	}

	db.CreateIndex()

	fmt.Printf("Starting version: 5\n")

	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/getThumbnail/", getThumbnailHandler)
	http.HandleFunc("/getImage/", getImageHandler)
	http.HandleFunc("/getImageCount/", getImageCountHandler)
	http.HandleFunc("/updateDatabase/", updateDatabaseHandler)
	http.ListenAndServe(":8080", nil)
}

// itob returns an 8-byte big endian representation of v.
func itob(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}

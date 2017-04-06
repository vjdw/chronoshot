package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"github.com/vjdw/chronoshot/db"

	"github.com/rjeczalik/notify"

	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/tiff"
)

// "github.com/nfnt/resize" replaced by "gopkg.in/h2non/bimg.v1"
// requires libvips to be installed

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	f, err := os.Open("./index.html")
	if err != nil {
		log.Fatal(err)
	}
	if _, err := io.Copy(w, f); err != nil {
		log.Fatal(err)
	}
}

func getAssetCountHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	response := strconv.Itoa(db.GetLengthOfIndex())
	w.Header().Set("Content-Length", strconv.Itoa(len(response)))
	if _, err := w.Write([]byte(response)); err != nil {
		log.Println("unable to write response.")
	}
}

func getAssetHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	index, err := strconv.ParseUint(id, 10, 64)
	if err != nil || index < 1 {
		log.Println(id, "is not a valid id.")
		http.NotFound(w, r)
		return
	}
	imgPath := db.GetAssetPathByIndex(index)
	log.Println("Requested asset:", string(imgPath))

	f, err := os.Open(string(imgPath[:]))
	if err != nil {
		log.Println("Could not open file", string(imgPath[:]))
		http.NotFound(w, r)
		return
	}

	var buf bytes.Buffer
	_, err = buf.ReadFrom(f)
	if err != nil {
		log.Println("Could not read file", string(imgPath[:]))
		http.NotFound(w, r)
		return
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
	}
	buf := db.GetThumbnailByIndex(index)

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
	if _, err := w.Write(buf); err != nil {
		log.Println("unable to write image.")
	}
}

func getExifDateTimeHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	index, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	dateTime := db.GetDateTimeByIndex(index)
	buf, err := json.Marshal(map[string]time.Time{"datetime": dateTime})
	if err != nil {
		log.Fatal(err)
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
	if _, err := w.Write(buf); err != nil {
		log.Println("unable to write response.")
	}
}

func updateDatabaseHandler(w http.ResponseWriter, r *http.Request) {

}

func watchDirectory(path string) {
	// Make the channel buffered to ensure no event is dropped. Notify will drop
	// an event if the receiver is not able to keep up the sending pace.
	c := make(chan notify.EventInfo, 1)

	// Set up a watchpoint listening for events within a directory tree rooted
	// at current working directory. Dispatch remove events to c.
	recursivePath := strings.Join([]string{path, "/..."}, "")
	if err := notify.Watch(recursivePath, c, notify.Create|notify.Rename|notify.Remove); err != nil {
		log.Fatal(err)
	}
	defer notify.Stop(c)

	// Block until an event is received.
	for {
		ei := <-c
		log.Println("Got event:", ei)
		go processPhoto(ei.Path(), nil, nil)
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

			if db.FilePathAdded([]byte(path)) {
				fmt.Printf("Already in database: %s\n", path)
				return
			}

			buf, err := ioutil.ReadFile(path)
			check(err)
			if len(buf) == 0 {
				fmt.Println("Could not process photo:", path, "because file is empty.")
				return
			}

			datetime, orientation := getExifDateTime(buf)
			assetDbKey := []byte(strings.Join([]string{datetime.String(), path}, "<#>"))

			err = storeThumbnail(assetDbKey, path, buf, orientation, datetime)
			if err != nil {
				fmt.Println("Could not process photo:", path, "because:", err)
			}
		}
	}(path)

	return nil
}

func getExifDateTime(b []byte) (time.Time, *tiff.Tag) {
	// f, err := os.Open(path)
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// Optionally register camera makenote data parsing - currently Nikon and
	// Canon are supported.
	//exif.RegisterParsers(mknote.All...)

	r := bytes.NewReader(b)

	x, err := exif.Decode(r)
	if err != nil {
		//log.Fatal(err)
		return time.Unix(0, 0), nil
	}
	orientation, err := x.Get(exif.Orientation)
	if err != nil {
		orientation = nil
	}
	//camModel, _ := x.Get(exif.Model) // normally, don't ignore errors!
	//fmt.Println(camModel.StringVal())

	//focal, _ := x.Get(exif.FocalLength)
	//numer, denom, _ := focal.Rat2(0) // retrieve first (only) rat. value
	//fmt.Printf("%v/%v", numer, denom)

	// Two convenience functions exist for date/time taken and GPS coords:
	tm, _ := x.DateTime()
	//fmt.Println("Taken: ", tm)

	//lat, long, _ := x.LatLong()
	//fmt.Println("lat, long: ", lat, ", ", long)

	return tm, orientation
}

func storeThumbnail(assetDbKey []byte, path string, b []byte, orientation *tiff.Tag, dateTime time.Time) error {
	fmt.Println("storeThumbnail for", path)

	r := bytes.NewReader(b)

	// decode jpeg into image.Image
	img, err := jpeg.Decode(r)
	if err != nil {
		return err
		//log.Fatal(err)
	}

	// Camera orientation, e.g. if orientation value is 8 then top of camera was point right.
	//    1
	//  6   8
	//    3
	if orientation != nil {
		if orientation.Val[0] == 8 {
			img = imaging.Rotate90(img)
		} else if orientation.Val[0] == 6 {
			img = imaging.Rotate270(img)
		} else if orientation.Val[0] == 3 {
			img = imaging.Rotate180(img)
		}
	}

	thumbnail := imaging.Thumbnail(img, 200, 200, imaging.Linear)
	//thumbnail := resize.Thumbnail(200, 200, img, resize.Bilinear)
	//thumbnail := resize.Resize(200, 0, img, resize.Lanczos3)

	buf := new(bytes.Buffer)
	jpeg.Encode(buf, thumbnail, &jpeg.Options{Quality: 75})
	db.PutAsset(assetDbKey, []byte(path), buf.Bytes(), dateTime)

	////////////////////
	// // Faster resize method, but seems to not like being multithreaded?
	// buffer, err := bimg.Read(path)
	// if err != nil {
	// 	return err
	// 	//log.Fatal(err)
	// }

	// thumbnail, err := bimg.NewImage(buffer).Thumbnail(200)
	// if err != nil {
	// 	log.Fatal(err)
	// }

	//db.PutAsset(assetDbKey, []byte(path), thumbnail, dateTime)

	return nil
}

func main() {
	fmt.Println("Starting chronoshot version: 11.")
	db.Init()

	//dir := "/home/vin/Desktop/scratch"
	//dir := "/media/data/photos"
	dir := "/home/vin/go/src/github.com/h2non/bimg/fixtures"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	fmt.Println("Photo directory set to", dir)

	ticker := time.NewTicker(30 * time.Second)
	quit := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				db.RebuildIndex()
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()

	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/getThumbnail/", getThumbnailHandler)
	http.HandleFunc("/getExifDateTime/", getExifDateTimeHandler)
	http.HandleFunc("/getAsset/", getAssetHandler)
	http.HandleFunc("/getAssetCount/", getAssetCountHandler)
	http.HandleFunc("/updateDatabase/", updateDatabaseHandler)
	go http.ListenAndServe(":8080", nil)
	fmt.Println("Webserver ready.")

	if err := filepath.Walk(dir, processPhoto); err != nil {
		log.Fatal(err)
	}
	// Flush out final workers.
	for i := 0; i < cap(rateLimiter); i++ {
		rateLimiter <- true
	}
	for i := 0; i < cap(rateLimiter); i++ {
		<-rateLimiter
	}

	fmt.Println("Watching for new images in", dir)
	watchDirectory(dir)
}

// itob returns an 8-byte big endian representation of v.
func itob(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}

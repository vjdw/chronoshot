package db

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"log"
	"time"

	"strings"

	"github.com/boltdb/bolt"
)

type assetKvp struct {
	Key   []byte
	Value assetInfo
}

type assetInfo struct {
	KeyHash   []byte
	Path      []byte
	Thumbnail []byte
	DateTime  time.Time
}

type selection struct {
	AssetKey   []byte
	IsSelected bool
}

var chanPutAsset = make(chan assetKvp)
var chanRebuildIndex = make(chan bool)
var chanPutSelection = make(chan selection)
var isDirtyIndex = true

func Init() {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	err = db.Update(func(tx *bolt.Tx) error {
		_, err = tx.CreateBucketIfNotExists([]byte("fileIndex"))
		if err != nil {
			return err
		}
		// _, err := tx.CreateBucketIfNotExists([]byte("viewIndex"))
		// if err != nil {
		// 	return err
		// }
		_, err = tx.CreateBucketIfNotExists([]byte("assets"))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("assetsLookup"))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("selections"))
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	db.Close()

	rebuildIndex()

	go writeChannelsMonitor()
}

func writeChannelsMonitor() {
	for {
		select {
		case assetKvp := <-chanPutAsset:
			putAsset(assetKvp)
			//fmt.Printf("Put in database: %s\n", assetKvp.Key)
		case <-chanRebuildIndex:
			rebuildIndex()
		case selection := <-chanPutSelection:
			putSelection(selection)
		}
	}
}

func PutAsset(key []byte, path []byte, thumbnail []byte, dateTime time.Time) {

	hasher := md5.New()
	hasher.Write(key)
	keyHash := hasher.Sum(nil)
	keyHashStr := []byte(base64.URLEncoding.EncodeToString(keyHash))

	chanPutAsset <- assetKvp{key, assetInfo{keyHashStr, path, thumbnail, dateTime}}
}

func putAsset(kvp assetKvp) {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()

	err = db.Update(func(tx *bolt.Tx) error {
		isDirtyIndex = true

		hasher := md5.New()
		hasher.Write(kvp.Key)
		keyHash := hasher.Sum(nil)
		keyHashStr := base64.URLEncoding.EncodeToString(keyHash)

		b := tx.Bucket([]byte("assets"))
		if err != nil {
			return err
		}
		serialisedAssetInfo, err := serialise(kvp.Value)
		if err != nil {
			log.Fatal(err)
		}
		err = b.Put(kvp.Key, serialisedAssetInfo)
		if err != nil {
			log.Fatal(err)
		}

		b = tx.Bucket([]byte("assetsLookup"))
		if err != nil {
			return err
		}
		err = b.Put([]byte(keyHashStr), kvp.Key)
		if err != nil {
			log.Fatal(err)
		}

		return err
	})
	db.Close()

	if err != nil {
		log.Fatal(err)
	}
}

func PutSelection(assetKey []byte, isSelected bool) {
	chanPutSelection <- selection{assetKey, isSelected}
}

func putSelection(s selection) {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()

	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("selections"))
		if err != nil {
			return err
		}

		if s.IsSelected {
			serialisedSelection, err := serialise(s.IsSelected)
			if err != nil {
				log.Fatal(err)
			}
			return b.Put(s.AssetKey, serialisedSelection)
		} else {
			return b.Delete(s.AssetKey)
		}
	})
	db.Close()

	if err != nil {
		log.Fatal(err)
	}
}

func serialise(key interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(key)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func deserialiseAssetInfo(key []byte) (assetInfo, error) {
	buf := bytes.NewBuffer(key)
	var t assetInfo
	dec := gob.NewDecoder(buf)
	err := dec.Decode(&t)
	if err != nil {
		return assetInfo{}, err
	}
	return t, nil
}

func GetThumbnail(key []byte) []byte {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var buf []byte
	err = db.View(func(tx *bolt.Tx) error {

		assetsLookup := tx.Bucket([]byte("assetsLookup"))
		assetKey := assetsLookup.Get(key)

		b := tx.Bucket([]byte("assets"))
		info, err := deserialiseAssetInfo(b.Get(assetKey))
		if err != nil {
			log.Fatal(err)
		}
		buf = info.Thumbnail
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	// buf is lost on return because of db.Close()
	bufCopy := make([]byte, len(buf), (cap(buf)+1)*2)
	copy(bufCopy, buf)
	return bufCopy
}

func KeyExists(key []byte) bool {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	exists := false
	err = db.View(func(tx *bolt.Tx) error {

		assetsLookup := tx.Bucket([]byte("assetsLookup"))
		assetKey := assetsLookup.Get(key)
		exists = assetKey != nil
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	return exists
}

func FilePathAdded(filepath []byte) bool {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var buf []byte
	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("fileIndex"))
		if b != nil {
			buf = b.Get(filepath)
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	return buf != nil
}

// func GetThumbnailByIndex(i uint64) []byte {
// 	db, err := bolt.Open("chronoshot.db", 0777, nil)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	defer db.Close()

// 	var buf []byte
// 	err = db.View(func(tx *bolt.Tx) error {
// 		viewIndex := tx.Bucket([]byte("viewIndex"))
// 		assetKey := viewIndex.Get(itob(i))

// 		if assetKey != nil {
// 			assets := tx.Bucket([]byte("assets"))
// 			info, err := deserialiseAssetInfo(assets.Get(assetKey))
// 			if err != nil {
// 				log.Fatal(err)
// 			}
// 			buf = info.Thumbnail
// 		}
// 		return nil
// 	})
// 	if err != nil {
// 		log.Fatal(err)
// 	}

// 	// buf is lost on return because of db.Close()
// 	bufCopy := make([]byte, len(buf), (cap(buf)+1)*2)
// 	copy(bufCopy, buf)
// 	return bufCopy
// }

// func GetDateTimeByIndex(i uint64) time.Time {
// 	db, err := bolt.Open("chronoshot.db", 0777, nil)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	defer db.Close()

// 	var dateTime time.Time
// 	err = db.View(func(tx *bolt.Tx) error {

// 		viewIndex := tx.Bucket([]byte("viewIndex"))
// 		assetKey := viewIndex.Get(itob(i))

// 		if assetKey != nil {
// 			assets := tx.Bucket([]byte("assets"))
// 			info, err := deserialiseAssetInfo(assets.Get(assetKey))
// 			if err != nil {
// 				log.Fatal(err)
// 			}
// 			dateTime = info.DateTime
// 		}
// 		return nil
// 	})
// 	if err != nil {
// 		log.Fatal(err)
// 	}

// 	return dateTime
// }

func GetDateTime(key []byte) time.Time {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var dateTime time.Time
	err = db.View(func(tx *bolt.Tx) error {
		if key != nil {
			assetsLookup := tx.Bucket([]byte("assetsLookup"))
			assetKey := assetsLookup.Get(key)

			assets := tx.Bucket([]byte("assets"))
			info, err := deserialiseAssetInfo(assets.Get(assetKey))
			if err != nil {
				log.Fatal(err)
			}
			dateTime = info.DateTime
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	return dateTime
}

// func GetIsSelectedByIndex(i uint64) bool {
// 	db, err := bolt.Open("chronoshot.db", 0777, nil)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	defer db.Close()

// 	var isSelected bool
// 	err = db.View(func(tx *bolt.Tx) error {
// 		selections := tx.Bucket([]byte("selections"))
// 		isSelected = selections.Get(itob(i)) != nil
// 		return nil
// 	})
// 	if err != nil {
// 		log.Fatal(err)
// 	}

// 	return isSelected
// }

func GetIsSelected(key []byte) bool {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var isSelected bool
	err = db.View(func(tx *bolt.Tx) error {
		selections := tx.Bucket([]byte("selections"))
		isSelected = selections.Get(key) != nil
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	return isSelected
}

// func GetAssetPathByIndex(i uint64) []byte {
// 	db, err := bolt.Open("chronoshot.db", 0777, nil)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	defer db.Close()

// 	var assetKey []byte
// 	var assetValue []byte
// 	err = db.View(func(tx *bolt.Tx) error {

// 		viewIndex := tx.Bucket([]byte("viewIndex"))
// 		assetKey = viewIndex.Get(itob(i))

// 		assets := tx.Bucket([]byte("assets"))
// 		info, err := deserialiseAssetInfo(assets.Get(assetKey))
// 		if err != nil {
// 			log.Fatal(err)
// 		}
// 		assetValue = info.Path

// 		return nil
// 	})
// 	if err != nil {
// 		log.Fatal(err)
// 	}

// 	// assetValue is lost on return because of db.Close()
// 	assetValueCopy := make([]byte, len(assetValue), (cap(assetValue)+1)*2)
// 	copy(assetValueCopy, assetValue)
// 	return assetValueCopy
// }

func GetAssetPath(key []byte) []byte {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var assetValue []byte
	err = db.View(func(tx *bolt.Tx) error {
		assetsLookup := tx.Bucket([]byte("assetsLookup"))
		assetKey := assetsLookup.Get(key)

		assets := tx.Bucket([]byte("assets"))
		info, err := deserialiseAssetInfo(assets.Get(assetKey))
		if err != nil {
			log.Fatal(err)
		}
		assetValue = info.Path

		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("xyzzy1", string(assetValue))
	// assetValue is lost on return because of db.Close()
	assetValueCopy := make([]byte, len(assetValue), (cap(assetValue)+1)*2)
	copy(assetValueCopy, assetValue)
	fmt.Println("xyzzy2", string(assetValueCopy))
	return assetValueCopy
}

func RebuildIndex() {
	// Queue up a call to rebuildIndex.
	chanRebuildIndex <- true
}

func rebuildIndex() {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {

		log.Fatal(err)
		fmt.Println("Ignoring request to rebuild index because it isn't dirty.")
	}
	defer func() {
		db.Close()
		fmt.Printf("%v items in index.\n", GetLengthOfIndex())
	}()

	err = db.Update(func(tx *bolt.Tx) error {
		if !isDirtyIndex {
			fmt.Println("Ignoring request to rebuild index because it isn't dirty.")
			return nil
		}
		fmt.Print("Rebuilding index... ")
		isDirtyIndex = false

		// viewIndex := tx.Bucket([]byte("viewIndex"))
		// err = viewIndex.ForEach(func(k, v []byte) error {
		// 	err := viewIndex.Delete(k)
		// 	if err != nil {
		// 		return err
		// 	}
		// 	return nil
		// })
		// if err != nil {
		// 	return err
		// }

		fileIndex := tx.Bucket([]byte("fileIndex"))
		err = fileIndex.ForEach(func(k, v []byte) error {
			err := fileIndex.Delete(k)
			if err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return err
		}

		assets := tx.Bucket([]byte("assets"))
		//assetsCount, err := getLengthOfBucket(db, "assets")
		if err != nil {
			log.Fatal(err)
		}

		// Iterate over items in sorted key order.
		// Put in viewIndex in reverse datetime order.
		indexCount := uint64(1)
		if err := assets.ForEach(func(k, v []byte) error {
			filepath := strings.Split(string(k), "<#>")[1]
			fileIndex.Put([]byte(filepath), k)
			//viewIndex.Put(itob(uint64(assetsCount)-indexCount), k)
			indexCount++
			return nil
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}

func GetAllAssetKeys() []string {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	assetsCount, err := getLengthOfBucket(db, "selections")
	allAssetKeys := make([]string, assetsCount)

	db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("assets"))
		i := 1
		b.ForEach(func(k, v []byte) error {
			info, err := deserialiseAssetInfo(v)
			if err != nil {
				log.Fatal(err)
			}
			//fmt.Printf("key=%s, value=%s\n", k, v)
			allAssetKeys[assetsCount-i] = string(info.KeyHash)
			i++
			return nil
		})
		return nil
	})

	return allAssetKeys
}

func GetLengthOfIndex() int {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	lengthOfBucket, err := getLengthOfBucket(db, "assetsLookup")

	if err != nil {
		fmt.Println("Error in GetLengthOfIndex getting length of index bucket", err)
		log.Fatal(err)
	}
	return lengthOfBucket
}

func getLengthOfBucket(db *bolt.DB, bucketName string) (int, error) {
	var lengthOfIndex int
	err := db.View(func(tx *bolt.Tx) error {
		index := tx.Bucket([]byte(bucketName))
		lengthOfIndex = index.Stats().KeyN
		return nil
	})
	if err != nil {
		return 0, err
	}

	return lengthOfIndex, nil
}

// itob returns an 8-byte big endian representation of v.
func itob(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}

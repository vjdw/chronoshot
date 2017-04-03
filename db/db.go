package db

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"log"
	"time"

	"github.com/boltdb/bolt"
)

type assetKvp struct {
	Key   []byte
	Value assetInfo
}

type assetInfo struct {
	Path      []byte
	Thumbnail []byte
	DateTime  time.Time
}

var chanPutAsset = make(chan assetKvp)
var chanRebuildIndex = make(chan bool)
var isDirtyIndex = true

func Init() {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("index"))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("assets"))
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

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
		}
	}
}

func PutAsset(key []byte, path []byte, thumbnail []byte, dateTime time.Time) {
	chanPutAsset <- assetKvp{key, assetInfo{path, thumbnail, dateTime}}
}

func putAsset(kvp assetKvp) {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()

	err = db.Update(func(tx *bolt.Tx) error {
		isDirtyIndex = true
		b := tx.Bucket([]byte("assets"))
		if err != nil {
			return err
		}
		serialisedAssetInfo, err := serialise(kvp.Value)
		if err != nil {
			log.Fatal(err)
		}

		return b.Put(kvp.Key, serialisedAssetInfo)
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
		b := tx.Bucket([]byte("assets"))
		info, err := deserialiseAssetInfo(b.Get(key))
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

	var buf []byte
	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("assets"))
		if b != nil {
			buf = b.Get(key)
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	return buf != nil
}

func GetThumbnailByIndex(i uint64) []byte {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var buf []byte
	err = db.View(func(tx *bolt.Tx) error {
		index := tx.Bucket([]byte("index"))
		assetKey := index.Get(itob(i))

		if assetKey != nil {
			assets := tx.Bucket([]byte("assets"))
			info, err := deserialiseAssetInfo(assets.Get(assetKey))
			if err != nil {
				log.Fatal(err)
			}
			buf = info.Thumbnail
		}
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

func GetDateTimeByIndex(i uint64) time.Time {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var dateTime time.Time
	err = db.View(func(tx *bolt.Tx) error {

		index := tx.Bucket([]byte("index"))
		assetKey := index.Get(itob(i))

		if assetKey != nil {
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

func GetAssetPathByIndex(i uint64) []byte {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var assetKey []byte
	var assetValue []byte
	err = db.View(func(tx *bolt.Tx) error {

		index := tx.Bucket([]byte("index"))
		assetKey = index.Get(itob(i))

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

	// assetValue is lost on return because of db.Close()
	assetValueCopy := make([]byte, len(assetValue), (cap(assetValue)+1)*2)
	copy(assetValueCopy, assetValue)
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
		fmt.Printf("%v items added to index.\n", GetLengthOfIndex())
	}()

	err = db.Update(func(tx *bolt.Tx) error {
		if !isDirtyIndex {
			fmt.Println("Ignoring request to rebuild index because it isn't dirty.")
			return nil
		}
		fmt.Println("Rebuilding index.")
		isDirtyIndex = false

		index := tx.Bucket([]byte("index"))
		err = index.ForEach(func(k, v []byte) error {
			err := index.Delete(k)
			if err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return err
		}

		assets := tx.Bucket([]byte("assets"))
		assetsCount, err := getLengthOfBucket(db, "assets")
		if err != nil {
			log.Fatal(err)
		}

		// Iterate over items in sorted key order.
		// Put in index in reverse datetime order.
		indexCount := uint64(0)
		if err := assets.ForEach(func(k, v []byte) error {
			index.Put(itob(uint64(assetsCount)-indexCount), k)
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

func GetLengthOfIndex() int {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	lengthOfBucket, err := getLengthOfBucket(db, "index")
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

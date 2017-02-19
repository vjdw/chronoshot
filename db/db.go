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

var dbWriteChannel = make(chan assetKvp)

func Init() {
	go writeChannelMonitor()
}

func writeChannelMonitor() {
	for {
		assetInfo := <-dbWriteChannel
		putAsset(assetInfo)
		fmt.Printf("Put in database: %s\n", assetInfo.Key)
	}
}

func PutAsset(key []byte, path []byte, thumbnail []byte, dateTime time.Time) {
	dbWriteChannel <- assetKvp{key, assetInfo{path, thumbnail, dateTime}}
}

func putAsset(kvp assetKvp) {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("assets"))
		if err != nil {
			return err
		}
		serialisedAssetInfo, err := serialise(kvp.Value)
		if err != nil {
			log.Fatal(err)
		}
		return b.Put(kvp.Key, serialisedAssetInfo)
	})
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
	err = db.View(func(tx *bolt.Tx) error {
		index := tx.Bucket([]byte("index"))
		assetKey = index.Get(itob(i))

		assets := tx.Bucket([]byte("assets"))
		info, err := deserialiseAssetInfo(assets.Get(assetKey))
		if err != nil {
			log.Fatal(err)
		}
		assetKey = info.Path

		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	// assetKey is lost on return because of db.Close()
	assetKeyCopy := make([]byte, len(assetKey), (cap(assetKey)+1)*2)
	copy(assetKeyCopy, assetKey)
	return assetKeyCopy
}

func CreateIndex() {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		fmt.Printf("Finished creating index.\n")
		db.Close()
	}()

	// index needs to be deleted before it's recreated.
	err = db.Update(func(tx *bolt.Tx) error {
		err = tx.DeleteBucket([]byte("index"))
		if err != bolt.ErrBucketNotFound {
			return err
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		assets, err := tx.CreateBucketIfNotExists([]byte("assets"))
		if err != nil {
			return err
		}
		index, err := tx.CreateBucket([]byte("index"))
		if err != nil {
			return err
		}

		assetsCount, err := getLengthOfBucket(db, "assets")
		if err != nil {
			log.Fatal(err)
		}

		// Iterate over items in sorted key order.
		// Put in index in reverse datetime order.
		if err := assets.ForEach(func(k, v []byte) error {
			id, _ := index.NextSequence()
			fmt.Println("Adding to index", string(uint64(assetsCount)-id), string(k))
			index.Put(itob(uint64(assetsCount)-id), k)
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

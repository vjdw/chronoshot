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
var chanPutSelection = make(chan selection)

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
		_, err = tx.CreateBucketIfNotExists([]byte("assets"))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("assetsLookup"))
		if err != nil {
			return err
		}

		// xyzzy - when handling multiple selection lists, "selections" and "all" should be replaced
		_, err = tx.CreateBucketIfNotExists([]byte("selections"))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("all"))
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	db.Close()

	go writeChannelsMonitor()
}

func writeChannelsMonitor() {
	for {
		select {
		case assetKvp := <-chanPutAsset:
			putAsset(assetKvp)
		case selection := <-chanPutSelection:
			putSelection(selection)
		}
	}
}

func PutAsset(path []byte, thumbnail []byte, dateTime time.Time) {

	key := []byte(strings.Join([]string{dateTime.String(), string(path)}, "<#>"))

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

		b = tx.Bucket([]byte("fileIndex"))
		if err != nil {
			return err
		}
		filepath := strings.Split(string(kvp.Key), "<#>")[1]
		err = b.Put([]byte(filepath), kvp.Key)
		if err != nil {
			log.Fatal(err)
		}

		b = tx.Bucket([]byte("all"))
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

	// assetValue is lost on return because of db.Close()
	assetValueCopy := make([]byte, len(assetValue), (cap(assetValue)+1)*2)
	copy(assetValueCopy, assetValue)
	return assetValueCopy
}

func GetAllAssetKeys(setName []byte) []string {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	setCount, err := getLengthOfBucket(db, string(setName))
	setKeys := make([]string, setCount)

	db.View(func(tx *bolt.Tx) error {
		bAssets := tx.Bucket([]byte("assets"))
		bSet := tx.Bucket(setName)
		i := 1

		// Need to enumerate assets bucket to force date order on the returned set.
		bAssets.ForEach(func(k, v []byte) error {
			info, err := deserialiseAssetInfo(v)
			if err != nil {
				log.Fatal(err)
			}
			if bSet.Get(info.KeyHash) != nil {
				setKeys[setCount-i] = string(info.KeyHash)
				i++
			}
			return nil
		})
		return nil
	})

	return setKeys
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

package main

import (
	"fmt"
	"github.com/flachnetz/startup/lib/httputil"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/julienschmidt/httprouter"
	"github.com/pkg/errors"
	"io/ioutil"
	"net/http"
	"time"
	"reflect"
	"github.com/flachnetz/startup/lib/mapper"
	"database/sql"
	"github.com/rcrowley/go-metrics"
)

const maxValueSize = 1024 * 256

func init() {
	mapper.CustomTypes[reflect.TypeOf(Token{})] = func(value string, target reflect.Value) error {
		u, err := uuid.Parse(value)
		if err != nil {
			return errors.WithMessage(err, "parsing uuid")
		}

		target.Set(reflect.ValueOf(Token(u)))
		return nil
	}

	mapper.CustomTypes[reflect.TypeOf(Key(""))] = func(value string, target reflect.Value) error {
		target.Set(reflect.ValueOf(Key(value)))
		return nil
	}

	httputil.ErrorMapping[ErrNoSuchKey] = http.StatusNotFound
	httputil.ErrorMapping[ErrVersionConflict] = http.StatusConflict
	httputil.ErrorMapping[ErrValueTooLarge] = http.StatusRequestEntityTooLarge
}

type API struct {
	kv KVStore
}

func (api API) RegisterTo(router *httprouter.Router) {
	router.GET("/token/:token/key/:key", api.GetValue())
	router.POST("/token/:token/key/:key/version/:version", api.PostValue())
}

func (api API) PostValue() httprouter.Handle {
	type requestValues struct {
		Token   Token `path:"token" validate:"required"`
		Key     Key   `path:"key" validate:"required"`
		Version int   `path:"version"`
	}

	type resultValues struct {
		Version int `json:"version"`
	}

	metricPutMeter := metrics.GetOrRegisterMeter("value.put", nil)
	metricSize := metrics.GetOrRegisterHistogram("value.size", nil, metrics.NewUniformSample(1024))
	metricTooLarge := metrics.GetOrRegisterMeter("value.toolarge", nil)
	metricValueConflict := metrics.GetOrRegisterMeter("value.version.conflict", nil)

	return func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		var opts requestValues
		httputil.ExtractAndCall(&opts, w, r, params, func() (interface{}, error) {
			metricPutMeter.Mark(1)
			metricSize.Update(r.ContentLength)

			if r.ContentLength > maxValueSize {
				metricTooLarge.Mark(1)
				return nil, ErrValueTooLarge
			}

			// read the complete value into memory
			payload, err := ioutil.ReadAll(r.Body)
			if err != nil {
				return nil, errors.WithMessage(err, "reading request body")
			}

			// store in database.
			updatedVersion, err := api.kv.Put(opts.Token, opts.Key, payload, opts.Version)

			// lets check how often this happens
			if err == ErrVersionConflict {
				metricValueConflict.Mark(1)
			}

			return resultValues{updatedVersion}, errors.WithMessage(err, "store in database")
		})
	}
}

func (api API) GetValue() httprouter.Handle {
	type requestValues struct {
		Token Token `path:"token" validate:"required"`
		Key   Key   `path:"key" validate:"required"`
	}

	type resultValues struct {
		Version int    `json:"version"`
		Value   []byte `json:"value"`
	}

	metricGetValueSuccess := metrics.GetOrRegisterMeter("value.get[success:true]", nil)
	metricGetValueFailure := metrics.GetOrRegisterMeter("value.get[success:false]", nil)

	return func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		var opts requestValues
		httputil.ExtractAndCall(&opts, w, r, params, func() (interface{}, error) {
			payload, version, err := api.kv.Get(opts.Token, opts.Key)
			if err != nil {
				metricGetValueFailure.Mark(1)
				return nil, errors.WithMessage(err, "getting value")
			}

			metricGetValueSuccess.Mark(1)
			return resultValues{version, payload}, err
		})
	}
}

func transaction(db *sqlx.DB, fn func(tx *sqlx.Tx) error) (err error) {
	tx, err := db.Beginx()
	if err != nil {
		return errors.WithMessage(err, "begin transaction")
	}

	defer func() {
		if r := recover(); r != nil {
			switch e := r.(type) {
			case error:
				err = e

			case fmt.Stringer:
				err = errors.New(e.String())

			default:
				err = fmt.Errorf("%v", err)
			}
		}

		if err == nil {
			err = errors.WithMessage(tx.Commit(), "commit transaction")
		} else {
			// ignore error in case of rollback, we want to
			// preserve the original error.
			tx.Rollback()
		}
	}()

	return fn(tx)
}

type Token uuid.UUID
type Key string

var ErrNoSuchKey = errors.New("no such key")
var ErrVersionConflict = errors.New("version conflict")
var ErrValueTooLarge = errors.New("value too large")

type KVStore struct {
	db *sqlx.DB
}

func (kv *KVStore) Put(token Token, key Key, value []byte, version int) (int, error) {
	var updatedVersion int

	err := transaction(kv.db, func(tx *sqlx.Tx) error {
		err := tx.Get(&updatedVersion, `
				INSERT INTO kv_data (token, key, version, created, payload)
				VALUES ($1, $2, $3+1, $4, $5)
				ON CONFLICT (token, key) DO UPDATE SET
					created=EXCLUDED.created, 
					payload=EXCLUDED.payload,
					version=EXCLUDED.version
					WHERE (kv_data.version=$3 OR $3=0)
				RETURNING kv_data.version`,
			uuid.UUID(token), string(key), version, time.Now(), value)

		// The only case in which we find no rows to update is that the
		// version mismatches.
		if err == sql.ErrNoRows {
			return ErrVersionConflict
		}

		return err
	})

	return updatedVersion, err
}

func (kv *KVStore) Get(token Token, key Key) ([]byte, int, error) {
	var result struct {
		Value   []byte `db:"payload"`
		Version int    `db:"version"`
	}

	// read the value form the database.
	err := transaction(kv.db, func(tx *sqlx.Tx) error {
		return tx.Get(&result,
			`SELECT payload, version FROM kv_data WHERE token=$1 AND key=$2`,
			uuid.UUID(token), string(key))
	})

	if err == sql.ErrNoRows {
		return nil, 0, ErrNoSuchKey
	}

	return result.Value, result.Version, err
}

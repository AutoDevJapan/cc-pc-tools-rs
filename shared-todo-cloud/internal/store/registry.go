package store

import (
	"bytes"
	"encoding/json"

	bolt "go.etcd.io/bbolt"
)

// Registry is auxiliary organization data. It stays separate from the original
// task document and its revision so reading or updating assignments cannot
// rewrite task ownership, dependencies or scheduler reservations.
type Registry struct {
	SHA256   string          `json:"sha256"`
	Document json.RawMessage `json:"document"`
}

func (s *Store) Registry(name string) (Registry, error) {
	var result Registry
	err := s.view(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("registries"))
		if b == nil || b.Get([]byte(name)) == nil {
			return &Error{Code: CodeNotFound, Reason: "registry"}
		}
		result.Document = bytes.Clone(b.Get([]byte(name)))
		result.SHA256 = digest(result.Document)
		return nil
	})
	return result, err
}
func (s *Store) PutRegistry(name string, document json.RawMessage, previous string) (Registry, error) {
	if identifier(name) != nil || len(document) == 0 || len(document) > 32<<20 {
		return Registry{}, invalid("invalid_registry")
	}
	if _, err := canonical(document); err != nil {
		return Registry{}, err
	}
	result := Registry{SHA256: digest(document), Document: bytes.Clone(document)}
	err := s.write(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("registries"))
		if err != nil {
			return err
		}
		old := b.Get([]byte(name))
		if old != nil && digest(old) == result.SHA256 {
			return nil
		}
		if old != nil && previous != digest(old) {
			return &Error{Code: CodeConflict, Reason: "registry_changed"}
		}
		if old == nil && previous != "" {
			return &Error{Code: CodeConflict, Reason: "registry_changed"}
		}
		return b.Put([]byte(name), document)
	})
	if err != nil {
		return Registry{}, err
	}
	return result, nil
}

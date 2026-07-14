// Block-clone NEGATIVE fixture (negative/errcheck-chain-go), review §5.3.
// The only cross-file commonality is Go error-check boilerplate: four
// consecutive `if err != nil { return ... }` stanzas whose initializer
// shapes differ. The longest verbatim token run is well under 8 lines,
// so no block finding may be produced at min-block-lines 8.
package fixture

import "encoding/json"

func persistSnapshot(s *Snapshot, store BlobStore, db Tx) error {
	blob, err := json.Marshal(s.State)
	if err != nil {
		return err
	}
	if err := store.Write(s.Key, blob); err != nil {
		return err
	}
	if err := store.Sync(); err != nil {
		return err
	}
	res, err := db.Exec(insertSnapshot, s.Key, checksum(blob))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errSnapshotConflict
	}
	s.persistedAt = clockNow()
	auditLog.Printf("snapshot %s persisted (%d bytes)", s.Key, len(blob))
	return nil
}

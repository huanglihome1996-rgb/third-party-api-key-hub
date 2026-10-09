package sqlite

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
)

// SQLite may ignore an invalid recovery header or checksum and then report a
// healthy older database (or leave uncommitted rollback pages). Check recovery
// evidence before the first connection can delete or checkpoint those files.
func validateRecoveryFiles(path string) error {
	if err := validateWAL(path + "-wal"); err != nil {
		return err
	}
	return validateJournal(path + "-journal")
}

func validateWAL(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	header := make([]byte, 32)
	n, err := io.ReadFull(f, header)
	if err == io.EOF && n == 0 {
		return nil
	}
	if err != nil {
		return errors.New("WAL header rejected")
	}
	magic := binary.BigEndian.Uint32(header[:4])
	pageSize := binary.BigEndian.Uint32(header[8:12])
	if (magic != 0x377f0682 && magic != 0x377f0683) || binary.BigEndian.Uint32(header[4:8]) != 3007000 || !validPageSize(pageSize) {
		return errors.New("WAL header rejected")
	}
	var order binary.ByteOrder = binary.LittleEndian
	if magic == 0x377f0683 {
		order = binary.BigEndian
	}
	s1, s2 := walChecksum(header[:24], order, 0, 0)
	if s1 != binary.BigEndian.Uint32(header[24:28]) || s2 != binary.BigEndian.Uint32(header[28:32]) {
		return errors.New("WAL header checksum rejected")
	}
	frame := make([]byte, 24+int(pageSize))
	validatedPrefix := false
	pendingSuffix := false
	for {
		n, readErr := io.ReadFull(f, frame)
		if readErr == io.EOF {
			return nil
		}
		if readErr == io.ErrUnexpectedEOF {
			// Only an identifiable pending frame is safe to ignore. A partial
			// frame with a commit marker (or too little header to establish its
			// generation) may be a damaged durable commit.
			if n >= 16 && binary.BigEndian.Uint32(frame[:4]) != 0 && binary.BigEndian.Uint32(frame[4:8]) == 0 && bytes.Equal(frame[8:16], header[16:24]) {
				return nil
			}
			return errors.New("partial WAL commit or unknown frame rejected")
		}
		if readErr != nil {
			return readErr
		}
		next1, next2 := walChecksum(frame[:8], order, s1, s2)
		next1, next2 = walChecksum(frame[24:], order, next1, next2)
		checksumMatches := next1 == binary.BigEndian.Uint32(frame[16:20]) && next2 == binary.BigEndian.Uint32(frame[20:24])
		if !bytes.Equal(frame[8:16], header[16:24]) {
			// RESTART reuses a WAL without truncating its old suffix. Salt-1
			// advances and salt-2 changes on reset; older generations after the
			// validated current prefix are not part of the new transaction. That
			// prefix may be pending if the first transaction after reset crashed
			// after spilling pages but before committing.
			// Salts are excluded from frame checksums, so a matching current
			// checksum instead identifies damage to a current-generation frame.
			generation := binary.BigEndian.Uint32(header[16:20]) - binary.BigEndian.Uint32(frame[8:12])
			if validatedPrefix && !checksumMatches && generation > 0 && generation < 1<<31 && !bytes.Equal(frame[12:16], header[20:24]) {
				return nil
			}
			return errors.New("WAL generation rejected")
		}
		if binary.BigEndian.Uint32(frame[:4]) == 0 {
			return errors.New("WAL frame rejected")
		}
		if pendingSuffix {
			// A rollback can leave same-generation pending frames behind a
			// newer, smaller transaction. Their checksums belong to the rolled
			// back chain. Any later current-generation commit makes a broken
			// chain durable evidence and must still be rejected.
			if binary.BigEndian.Uint32(frame[4:8]) != 0 {
				return errors.New("WAL committed checksum chain rejected")
			}
			continue
		}
		if !checksumMatches {
			if validatedPrefix && binary.BigEndian.Uint32(frame[4:8]) == 0 {
				pendingSuffix = true
				continue
			}
			return errors.New("WAL frame checksum rejected")
		}
		s1, s2 = next1, next2
		validatedPrefix = true
	}
}

func walChecksum(raw []byte, order binary.ByteOrder, s1, s2 uint32) (uint32, uint32) {
	for i := 0; i < len(raw); i += 8 {
		s1 += order.Uint32(raw[i:i+4]) + s2
		s2 += order.Uint32(raw[i+4:i+8]) + s1
	}
	return s1, s2
}

func validPageSize(size uint32) bool { return size >= 512 && size <= 65536 && size&(size-1) == 0 }

func validateJournal(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	header := make([]byte, 28)
	var offset int64
	var page, sector, originalPages uint32
	var record []byte
	for offset < info.Size() {
		n, readErr := f.ReadAt(header, offset)
		// A genuinely cold chunk follows unsynced journal writes; SQLite has
		// not written pages protected by that chunk into the database.
		if n >= 8 && bytes.Equal(header[:8], make([]byte, 8)) {
			return nil
		}
		if readErr != nil || !bytes.Equal(header[:8], []byte{0xd9, 0xd5, 0x05, 0xf9, 0x20, 0xa1, 0x63, 0xd7}) {
			return errors.New("rollback journal header rejected")
		}
		if offset == 0 {
			sector = binary.BigEndian.Uint32(header[20:24])
			page = binary.BigEndian.Uint32(header[24:28])
			originalPages = binary.BigEndian.Uint32(header[16:20])
			if sector < 32 || sector > 65536 || sector&(sector-1) != 0 || !validPageSize(page) || originalPages == 0 {
				return errors.New("rollback journal header rejected")
			}
			record = make([]byte, int(page)+8)
		} else if binary.BigEndian.Uint32(header[16:20]) != originalPages {
			return errors.New("rollback journal database size rejected")
		}
		count := int64(binary.BigEndian.Uint32(header[8:12]))
		seed := binary.BigEndian.Uint32(header[12:16])
		offset += int64(sector)
		if offset > info.Size() {
			return errors.New("rollback journal header truncated")
		}
		if count == 0xffffffff {
			if (info.Size()-offset)%int64(len(record)) != 0 {
				return errors.New("rollback journal record truncated")
			}
			count = (info.Size() - offset) / int64(len(record))
		}
		if count == 0 && offset < info.Size() {
			return errors.New("empty hot rollback chunk rejected")
		}
		if count > (info.Size()-offset)/int64(len(record)) {
			return errors.New("rollback journal record truncated")
		}
		for i := int64(0); i < count; i++ {
			if _, err = f.ReadAt(record, offset); err != nil {
				return err
			}
			id := binary.BigEndian.Uint32(record[:4])
			sum := seed
			for j := int(page) - 200; j > 0; j -= 200 {
				sum += uint32(record[4+j])
			}
			if id == 0 || id > originalPages || sum != binary.BigEndian.Uint32(record[4+int(page):]) {
				return errors.New("rollback journal record checksum rejected")
			}
			offset += int64(len(record))
		}
		// Later hot headers are sector-aligned and have their own count/seed.
		offset = (offset + int64(sector) - 1) / int64(sector) * int64(sector)
	}
	return nil
}

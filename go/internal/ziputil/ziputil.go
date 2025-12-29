package ziputil

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

const (
	winzipAesExtraID = 0x9901
	aesAuthCodeLen   = 10
	zipCryptoHeader  = 12
)

// Portions adapted from github.com/yeka/zip (MIT license).

var (
	ErrDecryption     = errors.New("zip: decryption error")
	ErrPassword       = errors.New("zip: invalid password")
	ErrAuthentication = errors.New("zip: authentication failed")
)

type ReadOptions struct {
	LogPasswords bool
}

func IsEncrypted(file *zip.File) bool {
	if file == nil {
		return false
	}
	return file.Flags&0x1 == 1
}

func EffectiveMethod(file *zip.File) uint16 {
	if file == nil {
		return 0
	}
	if aesInfo, ok := parseAESExtra(file.Extra); ok && aesInfo.method != 0 {
		return aesInfo.method
	}
	return file.Method
}

func ReadFile(file *zip.File, passwords []string) ([]byte, error) {
	return ReadFileWithOptions(file, passwords, ReadOptions{})
}

func ReadFileWithOptions(file *zip.File, passwords []string, opts ReadOptions) ([]byte, error) {
	if file == nil {
		return nil, errors.New("zip file is nil")
	}
	if !IsEncrypted(file) {
		handle, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer handle.Close()
		return io.ReadAll(handle)
	}
	if len(passwords) == 0 {
		return nil, errors.New("zip entry is encrypted but no passwords provided")
	}

	raw, err := readRaw(file)
	if err != nil {
		return nil, err
	}
	aesInfo, aesOK := parseAESExtra(file.Extra)

	var lastErr error
	var attemptErrors []string
	var attemptPasswords []string
	attempts := 0
	for _, password := range passwords {
		password = strings.TrimSpace(password)
		if password == "" {
			continue
		}
		attempts++
		if opts.LogPasswords {
			attemptPasswords = append(attemptPasswords, password)
		}
		data, err := decryptAndDecompress(file, raw, password, aesInfo, aesOK)
		if err == nil {
			return data, nil
		}
		lastErr = err
		attemptErrors = append(attemptErrors, classifyErr(err))
	}
	if lastErr == nil {
		lastErr = errors.New("zip passwords exhausted")
	}
	if len(attemptErrors) > 0 {
		message := fmt.Sprintf(
			"zip password check failed after %d attempt(s): %s",
			attempts,
			strings.Join(attemptErrors, "; "),
		)
		if opts.LogPasswords && len(attemptPasswords) > 0 {
			message = fmt.Sprintf("%s (passwords=%q)", message, attemptPasswords)
		}
		return nil, errors.New(message)
	}
	return nil, fmt.Errorf("zip password check failed after %d attempt(s): %w", attempts, lastErr)
}

type aesExtra struct {
	ae       uint16
	strength byte
	method   uint16
}

func parseAESExtra(extra []byte) (aesExtra, bool) {
	for len(extra) >= 4 {
		tag := binary.LittleEndian.Uint16(extra[:2])
		size := binary.LittleEndian.Uint16(extra[2:4])
		extra = extra[4:]
		if int(size) > len(extra) {
			return aesExtra{}, false
		}
		if tag == winzipAesExtraID && size >= 7 {
			info := aesExtra{
				ae:       binary.LittleEndian.Uint16(extra[:2]),
				strength: extra[4],
				method:   binary.LittleEndian.Uint16(extra[5:7]),
			}
			return info, true
		}
		extra = extra[size:]
	}
	return aesExtra{}, false
}

func readRaw(file *zip.File) ([]byte, error) {
	if file == nil {
		return nil, errors.New("zip file is nil")
	}
	reader, err := file.OpenRaw()
	if err != nil {
		return nil, err
	}
	return io.ReadAll(reader)
}

func decryptAndDecompress(file *zip.File, raw []byte, password string, aesInfo aesExtra, aesOK bool) ([]byte, error) {
	if aesOK {
		if aesInfo.method == 0 {
			return nil, zip.ErrAlgorithm
		}
		compressed, err := decryptAES(raw, password, aesInfo)
		if err != nil {
			return nil, err
		}
		data, err := decompress(aesInfo.method, compressed)
		if err != nil {
			return nil, err
		}
		if aesInfo.ae != 2 {
			if err := verifyCRC(file, data); err != nil {
				return nil, err
			}
		}
		return data, nil
	}
	if file.Method == 99 {
		return nil, zip.ErrAlgorithm
	}

	compressed, err := decryptZipCrypto(raw, password)
	if err != nil {
		return nil, err
	}
	data, err := decompress(file.Method, compressed)
	if err != nil {
		return nil, err
	}
	if err := verifyCRC(file, data); err != nil {
		return nil, err
	}
	return data, nil
}

func decryptZipCrypto(raw []byte, password string) ([]byte, error) {
	if len(raw) < zipCryptoHeader {
		return nil, ErrDecryption
	}
	z := newZipCrypto([]byte(password))
	plain := z.decrypt(raw)
	if len(plain) < zipCryptoHeader {
		return nil, ErrDecryption
	}
	return plain[zipCryptoHeader:], nil
}

func decryptAES(raw []byte, password string, info aesExtra) ([]byte, error) {
	keyLen := aesKeyLen(info.strength)
	saltLen := keyLen / 2
	if keyLen == 0 || saltLen == 0 {
		return nil, ErrDecryption
	}
	minLen := saltLen + 2 + aesAuthCodeLen
	if len(raw) < minLen {
		return nil, ErrDecryption
	}
	salt := raw[:saltLen]
	pwvv := raw[saltLen : saltLen+2]
	ciphertext := raw[saltLen+2 : len(raw)-aesAuthCodeLen]
	authcode := raw[len(raw)-aesAuthCodeLen:]

	encKey, authKey, pwv := generateKeys([]byte(password), salt, keyLen)
	if !checkPasswordVerification(pwvv, pwv) {
		return nil, ErrPassword
	}
	if !checkAuthentication(authKey, ciphertext, authcode) {
		return nil, ErrAuthentication
	}
	return decryptCTR(encKey, ciphertext)
}

func decompress(method uint16, data []byte) ([]byte, error) {
	switch method {
	case zip.Store:
		return data, nil
	case zip.Deflate:
		reader := flate.NewReader(bytes.NewReader(data))
		defer reader.Close()
		return io.ReadAll(reader)
	default:
		return nil, zip.ErrAlgorithm
	}
}

func verifyCRC(file *zip.File, data []byte) error {
	if file == nil {
		return errors.New("zip file is nil")
	}
	if file.CRC32 == 0 {
		return nil
	}
	sum := crc32.ChecksumIEEE(data)
	if sum != file.CRC32 {
		return zip.ErrChecksum
	}
	return nil
}

func classifyErr(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrPassword):
		return "invalid password"
	case errors.Is(err, ErrAuthentication):
		return "authentication failed"
	case errors.Is(err, ErrDecryption):
		return "decryption failed"
	case errors.Is(err, zip.ErrChecksum):
		return "checksum error (likely wrong password)"
	case errors.Is(err, zip.ErrAlgorithm):
		return "unsupported compression algorithm"
	default:
		return err.Error()
	}
}

type zipCrypto struct {
	password []byte
	keys     [3]uint32
}

func newZipCrypto(passphrase []byte) *zipCrypto {
	z := &zipCrypto{password: passphrase}
	z.init()
	return z
}

func (z *zipCrypto) init() {
	z.keys[0] = 0x12345678
	z.keys[1] = 0x23456789
	z.keys[2] = 0x34567890
	for i := 0; i < len(z.password); i++ {
		z.updateKeys(z.password[i])
	}
}

func (z *zipCrypto) updateKeys(byteValue byte) {
	z.keys[0] = crc32update(z.keys[0], byteValue)
	z.keys[1] += z.keys[0] & 0xff
	z.keys[1] = z.keys[1]*134775813 + 1
	z.keys[2] = crc32update(z.keys[2], byte(z.keys[1]>>24))
}

func (z *zipCrypto) magicByte() byte {
	t := z.keys[2] | 2
	return byte((t * (t ^ 1)) >> 8)
}

func (z *zipCrypto) decrypt(ciphertext []byte) []byte {
	plain := make([]byte, len(ciphertext))
	for i, c := range ciphertext {
		v := c ^ z.magicByte()
		z.updateKeys(v)
		plain[i] = v
	}
	return plain
}

func crc32update(pCrc32 uint32, bval byte) uint32 {
	return crc32.IEEETable[(pCrc32^uint32(bval))&0xff] ^ (pCrc32 >> 8)
}

type ctr struct {
	b       cipher.Block
	ctr     []byte
	out     []byte
	outUsed int
}

const streamBufferSize = 512

func newWinZipCTR(block cipher.Block) cipher.Stream {
	bufSize := streamBufferSize
	if bufSize < block.BlockSize() {
		bufSize = block.BlockSize()
	}
	iv := make([]byte, block.BlockSize())
	iv[0] = 1
	return &ctr{
		b:       block,
		ctr:     iv,
		out:     make([]byte, 0, bufSize),
		outUsed: 0,
	}
}

func (x *ctr) refill() {
	remain := len(x.out) - x.outUsed
	if remain > x.outUsed {
		return
	}
	copy(x.out, x.out[x.outUsed:])
	x.out = x.out[:cap(x.out)]
	bs := x.b.BlockSize()
	for remain < len(x.out)-bs {
		x.b.Encrypt(x.out[remain:], x.ctr)
		remain += bs
		for i := 0; i < len(x.ctr); i++ {
			x.ctr[i]++
			if x.ctr[i] != 0 {
				break
			}
		}
	}
	x.out = x.out[:remain]
	x.outUsed = 0
}

func (x *ctr) XORKeyStream(dst, src []byte) {
	for len(src) > 0 {
		if x.outUsed >= len(x.out)-x.b.BlockSize() {
			x.refill()
		}
		n := xorBytes(dst, src, x.out[x.outUsed:])
		dst = dst[n:]
		src = src[n:]
		x.outUsed += n
	}
}

func xorBytes(dst, a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		dst[i] = a[i] ^ b[i]
	}
	return n
}

func aesKeyLen(strength byte) int {
	switch strength {
	case 1:
		return 16
	case 2:
		return 24
	case 3:
		return 32
	default:
		return 0
	}
}

func generateKeys(password, salt []byte, keySize int) (encKey, authKey, pwv []byte) {
	totalSize := (keySize * 2) + 2
	key := pbkdf2.Key(password, salt, 1000, totalSize, sha1.New)
	encKey = key[:keySize]
	authKey = key[keySize : keySize*2]
	pwv = key[keySize*2:]
	return
}

func checkPasswordVerification(pwvv, pwv []byte) bool {
	return subtle.ConstantTimeCompare(pwvv, pwv) > 0
}

func checkAuthentication(authKey, ciphertext, authcode []byte) bool {
	mac := hmac.New(sha1.New, authKey)
	_, _ = mac.Write(ciphertext)
	expected := mac.Sum(nil)
	expected = expected[:aesAuthCodeLen]
	return subtle.ConstantTimeCompare(expected, authcode) > 0
}

func decryptCTR(key []byte, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	stream := newWinZipCTR(block)
	out := make([]byte, len(ciphertext))
	stream.XORKeyStream(out, ciphertext)
	return out, nil
}

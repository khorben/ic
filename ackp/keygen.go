package ackp

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"github.com/unix4fun/ac/acutl"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/crypto/sha3"
	"hash"
	"io"
	//"fmt"
	//"encoding/json"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"os"
)

const (
	KEYRSA = iota
	KEYECDSA
	KEYEC25519
)

type IdentityKey struct {
	keyType int
	rsa     *rsa.PrivateKey
	ecdsa   *ecdsa.PrivateKey
	ec25519 *Ed25519PrivateKey
}

func (i *IdentityKey) Type() string {
	switch i.keyType {
	case KEYRSA:
		return "ac-rsa"
	case KEYECDSA:
		return "ac-ecdsa"
	case KEYEC25519:
		return "ac-ec25519"
	}
	return ""
}

func (i *IdentityKey) PubToPKIX(wr io.Writer) error {
	var err error
	var keyBin, keyHdr []byte

	switch i.keyType {
	case KEYRSA:
		keyBin, err = x509.MarshalPKIXPublicKey(i.rsa.Public())
		keyHdr = []byte("ac-rsa")
	case KEYECDSA:
		keyBin, err = x509.MarshalPKIXPublicKey(i.ecdsa.Public())
		keyHdr = []byte("ac-ecdsa")
	case KEYEC25519:
		keyBin, err = asn1.Marshal(i.ec25519.Pub[:])
		keyHdr = []byte("ac-25519")
	default:
		return errors.New("invalid key type")
	}

	if err != nil {
		return err
	}
	b64comp, err := acutl.CompressData(keyBin)
	if err != nil {
		return err
	}
	b64pub := acutl.B64EncodeData(b64comp)

	// let's write our stuff...
	wr.Write(keyHdr)
	wr.Write([]byte(" "))
	wr.Write(b64pub)
	// we're good
	return nil
}

func (i *IdentityKey) PrivToPKIX(wr io.Writer, passwd []byte) error {
	var keyHeader string
	var keyDer []byte
	var err error

	switch i.keyType {
	case KEYRSA:
		keyHeader = "RSA PRIVATE KEY"
		keyDer = x509.MarshalPKCS1PrivateKey(i.rsa)
	case KEYECDSA:
		keyHeader = "ECDSA PRIVATE KEY"
		keyDer, err = x509.MarshalECPrivateKey(i.ecdsa)
	case KEYEC25519:
		keyHeader = "EC25519 PRIVATE KEY"
		keyDer, err = asn1.Marshal(i.ec25519.Pub[:])
	default:
		return errors.New("invalid key type")
	}
	if err != nil {
		return err
	}
	pemKey, err := AEADEncryptPEMBlock(rand.Reader, keyHeader, keyDer, passwd)
	if err != nil {
		return err
	}
	return pem.Encode(wr, pemKey)
}

func (i *IdentityKey) ToKeyFiles(prefix string, passwd []byte) error {
	pubFile, err := os.OpenFile(prefix+".pub", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	defer pubFile.Close()
	if err != nil {
		return err
	}
	privFile, err := os.OpenFile(prefix, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0700)
	defer privFile.Close()
	if err != nil {
		return err
	}

	err = i.PubToPKIX(pubFile)
	if err != nil {
		return err
	}

	err = i.PrivToPKIX(privFile, passwd)
	if err != nil {
		return err
	}

	return nil
}

// will try to load fprefix.pub / fprefix
func FromKeyFiles(prefix string) (i *IdentityKey) {
	pubFile, err := os.OpenFile(prefix+".pub", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	defer pubFile.Close()
	if err != nil {
		return err
	}
	privFile, err := os.OpenFile(prefix, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0700)
	defer privFile.Close()
	if err != nil {
		return err
	}
	return nil
}

func NewIdentityKey(keytype int) (*IdentityKey, error) {
	var err error
	i := new(IdentityKey)

	switch keytype {
	case KEYRSA:
		i.keyType = keytype
		i.rsa, err = GenKeysRSA(rand.Reader)
	case KEYECDSA:
		i.keyType = keytype
		i.ecdsa, err = GenKeysECDSA(rand.Reader)
		/*
			//fmt.Printf("ECDSAAAAA: %v / %v\n", i.ecdsa, err)
			jsonProut, err := json.Marshal(i.ecdsa.Public())
			jsonTa, err := json.Marshal(i.ecdsa)
			fmt.Printf("ERROR: %s\n", err)
			b64comp, err := acutl.CompressData(jsonProut)
			b64pub := acutl.B64EncodeData(b64comp)
			fmt.Printf("JSON PublicKey: %s\n", jsonProut)
			fmt.Printf("JSON PublicKey: ac-ecdsa %s\n", b64pub)
			fmt.Printf("JSON AllKey: %s\n", jsonTa)

			pkixKey, err := x509.MarshalPKIXPublicKey(i.ecdsa.Public())
			if err != nil {
				panic(err)
			}
			b64comp, err = acutl.CompressData(pkixKey)
			b64pub = acutl.B64EncodeData(b64comp)
			fmt.Printf("PKIX PublicKey: ac-ecdsa %s\n", b64pub)
		*/

	case KEYEC25519:
		i.keyType = keytype
		i.ec25519, err = GenKeysED25519(rand.Reader)

		/*
			pkixKey, err := asn1.Marshal(i.ec25519.Pub[:])
			if err != nil {
				panic(err)
			}
			b64comp, err := acutl.CompressData(pkixKey)
			b64pub := acutl.B64EncodeData(b64comp)
			fmt.Printf("PKIX PublicKey: ac-ed25519 %s\n", b64pub)
		*/
	default:
		err = errors.New("invalid type")
		return nil, err
	}
	//fmt.Printf("C'EST BON ON A FINI\n")
	return i, nil
}

type SecretKeyGen struct {
	hash        func() hash.Hash
	channel     []byte
	nick        []byte
	server      []byte
	input       []byte
	input_pbkdf []byte
	//    prng []byte
	info_hkdf []byte
}

func (skgen *SecretKeyGen) Init(input []byte, channel []byte, nick []byte, serv []byte) (err error) {
	//skgen.hash = sha3.NewKeccak256
	// go.crypto changed it... mlgrmlbmlbm
	skgen.hash = sha3.New256

	if input != nil {
		skgen.input = make([]byte, len(input))
		copy(skgen.input, input)
	} else { // handle empty input with crypto/rand input
		skgen.input = make([]byte, 8192)
		_, err = io.ReadFull(rand.Reader, skgen.input)
		if err != nil {
			return err
		}
	}

	if channel != nil {
		skgen.channel = make([]byte, len(channel))
		copy(skgen.channel, channel)
	}

	if nick != nil {
		skgen.nick = make([]byte, len(nick))
		copy(skgen.nick, nick)
	}

	if serv != nil {
		skgen.server = make([]byte, len(serv))
		copy(skgen.server, serv)
	}

	prng := make([]byte, 256)
	_, err = io.ReadFull(rand.Reader, prng)
	if err != nil {
		return err
		//        fmt.Fprintf(os.Stderr, "POUET POUET Error")
		//        fmt.Println(err)
	}

	//    fmt.Fprintf(os.Stderr, "read %d random bytes\n", n)
	//dk := pbkdf2.Key([]byte("some password"), salt, 4096, 32, sha1.New)
	//func Key(password, salt []byte, iter, keyLen int, h func() hash.Hash) []byte
	// XXX TODO be sure of the PBKDF2 FUNCTION CALL ARGUMENTS...
	skgen.input_pbkdf = pbkdf2.Key(skgen.input, prng, 32768, 32, skgen.hash)
	//    fmt.Fprintf(os.Stderr, "PBKDF LEN: %d\n", len(skgen.input_pbkdf))

	// in Read() we will apply the HKDF function.. onto the PBKDF2 derived key.
	// XXX TODO: just to be sure implement HASH of each value instead of values
	// only.
	str_build := new(bytes.Buffer)
	str_build.Write(serv)
	str_build.WriteByte(byte(':'))
	str_build.Write(nick)
	str_build.WriteByte(byte(':'))
	str_build.Write(channel)

	skgen.info_hkdf, err = acutl.HashSHA3Data(str_build.Bytes())
	if err != nil {
		return err
	}

	return nil
}

// XXX TODO: return err if init() or Reset() has not been called
func (skgen *SecretKeyGen) Read(p []byte) (n int, err error) {
	prng := make([]byte, 256)
	n, err = io.ReadFull(rand.Reader, prng)
	if err != nil {
		//fmt.Fprintf(os.Stderr, "POUET POUET PROUT Error")
		//fmt.Println(err)
		return n, err
	}

	my_hkdf := hkdf.New(skgen.hash, skgen.input_pbkdf, prng, skgen.info_hkdf)
	n, err = io.ReadFull(my_hkdf, p)
	return n, err
}

// Package pow provides Proof-of-Work implementations.
// Consider using Proof-of-Work implementations prefixed with "Sync" to ensure
// that concurrent calls are synchronized (running at most one Proof-of-Work task at a time).
// The provided Proof-of-Work implementations allow the caller to supply a parallelism option,
// defining how many concurrent goroutines are used.
// If no parallelism option is supplied, then the number of CPU cores - 1 is used.
package pow

import "C"
import (
	. "github.com/iotaledger/iota.go/consts"
	"github.com/iotaledger/iota.go/curl"
	. "github.com/iotaledger/iota.go/transaction"
	. "github.com/iotaledger/iota.go/trinary"
	"sync"

	"github.com/pkg/errors"
	"math"
	"runtime"
	"time"
)

var (
	// ErrInvalidTrytesForProofOfWork gets returned when invalid trytes are supplied for PoW.
	ErrInvalidTrytesForProofOfWork = errors.New("invalid trytes supplied to Proof-of-Work func")
	// ErrUnknownProofOfWorkFunc gets returned when the wanted Proof-of-Work implementation is unknown.
	ErrUnknownProofOfWorkFunc = errors.New("unknown Proof-of-Work func")
)

// ProofOfWorkFunc is a function which given transaction trytes and a difficulty (called MWM), does the required amount of
// work to fulfill the difficulty requirement.
// The Proof-of-Work involves finding a nonce, which together with other elements of a transaction,
// result in a transaction hash with MWM-amount of 0s at the end of the hash.
// Given a MWM of 14, the hash of the transaction must have 14 zero trits at the end of the hash.
type ProofOfWorkFunc = func(trytes Trytes, mwm int, parallelism ...int) (Trytes, error)

// DoPoW computes the nonce field for each transaction so that the last MWM-length trits of the
// transaction hash are all zeroes. Starting from the 0 index transaction, the transactions get chained to
// each other through the trunk transaction hash field. The last transaction in the bundle approves
// the given branch and trunk transactions. This function also initializes the attachment timestamp fields.
// This function expects the passed in transaction trytes from highest to lowest index, meaning the transaction
// with current index 0 at the last position.
func DoPoW(trunkTx, branchTx Trytes, trytes []Trytes, mwm uint64, pow ProofOfWorkFunc) ([]Trytes, error) {
	txs, err := AsTransactionObjects(trytes, nil)
	if err != nil {
		return nil, err
	}

	var prev Trytes
	for i := 0; i < len(txs); i++ {
		switch {
		case i == 0:
			txs[i].TrunkTransaction = trunkTx
			txs[i].BranchTransaction = branchTx
		default:
			txs[i].TrunkTransaction = prev
			txs[i].BranchTransaction = trunkTx
		}

		txs[i].AttachmentTimestamp = time.Now().UnixNano() / 1000000
		txs[i].AttachmentTimestampLowerBound = LowerBoundAttachmentTimestamp
		txs[i].AttachmentTimestampUpperBound = UpperBoundAttachmentTimestamp

		var err error
		txs[i].Nonce, err = pow(MustTransactionToTrytes(&txs[i]), int(mwm))
		if err != nil {
			return nil, err
		}

		// set new transaction hash
		txs[i].Hash = TransactionHash(&txs[i])
		prev = txs[i].Hash
	}
	powedTxTrytes := MustTransactionsToTrytes(txs)
	return powedTxTrytes, nil
}

var (
	// contains the available Proof-of-Work implementation functions.
	proofOfWorkFuncs = make(map[string]ProofOfWorkFunc)
	// the default amount of parallel goroutines used during Proof-of-Work.
	defaultProofOfWorkParallelism int
)

func init() {
	proofOfWorkFuncs["Go"] = GoProofOfWork
	proofOfWorkFuncs["SyncGo"] = SyncGoProofOfWork
	defaultProofOfWorkParallelism = runtime.NumCPU()
	if defaultProofOfWorkParallelism != 1 {
		defaultProofOfWorkParallelism--
	}
}

// GetProofOfWorkImpl returns the specified Proof-of-Work implementation given a name.
func GetProofOfWorkImpl(name string) (ProofOfWorkFunc, error) {
	if p, exist := proofOfWorkFuncs[name]; exist {
		return p, nil
	}

	return nil, errors.Wrapf(ErrUnknownProofOfWorkFunc, "%s", name)
}

// GetProofOfWorkImplementations returns an array with the names of all available Proof-of-Work implementations.
func GetProofOfWorkImplementations() (powFuncNames []string) {
	powFuncNames = make([]string, len(proofOfWorkFuncs))

	i := 0
	for k := range proofOfWorkFuncs {
		powFuncNames[i] = k
		i++
	}

	return powFuncNames
}

// GetFastestProofOfWorkImpl returns the fastest Proof-of-Work implementation.
func GetFastestProofOfWorkImpl() (string, ProofOfWorkFunc) {
	orderPreference := []string{"SyncAVX", "SyncSSE", "SyncCARM64", "SyncC128", "SyncC"}

	for _, impl := range orderPreference {
		if p, exist := proofOfWorkFuncs[impl]; exist {
			return impl, p
		}
	}

	return "SyncGo", SyncGoProofOfWork
}

// GoProofOfWork does Proof-of-Work on the given trytes using only Go code.
func GoProofOfWork(trytes Trytes, mwm int, parallelism ...int) (Trytes, error) {
	return goProofOfWork(trytes, mwm, nil, parallelism...)
}

var syncGoProofOfWork = sync.Mutex{}

// SyncGoProofOfWork is like GoProofOfWork() but only runs one ongoing Proof-of-Work task at a time.
func SyncGoProofOfWork(trytes Trytes, mwm int, parallelism ...int) (Trytes, error) {
	syncGoProofOfWork.Lock()
	defer syncGoProofOfWork.Unlock()
	nonce, err := goProofOfWork(trytes, mwm, nil, parallelism...)
	if err != nil {
		return "", err
	}
	return nonce, nil
}

func proofOfWorkParallelism(parallelism ...int) int {
	if len(parallelism) != 0 && parallelism[0] > 0 {
		return parallelism[0]
	}
	return defaultProofOfWorkParallelism
}

// trytes
const (
	hBits uint64 = 0xFFFFFFFFFFFFFFFF
	lBits uint64 = 0x0000000000000000

	low0  uint64 = 0xDB6DB6DB6DB6DB6D
	high0 uint64 = 0xB6DB6DB6DB6DB6DB
	low1  uint64 = 0xF1F8FC7E3F1F8FC7
	high1 uint64 = 0x8FC7E3F1F8FC7E3F
	low2  uint64 = 0x7FFFE00FFFFC01FF
	high2 uint64 = 0xFFC01FFFF803FFFF
	low3  uint64 = 0xFFC0000007FFFFFF
	high3 uint64 = 0x003FFFFFFFFFFFFF

	nonceOffset         = HashTrinarySize - NonceTrinarySize
	nonceInitStart      = nonceOffset + 4
	nonceIncrementStart = nonceInitStart + NonceTrinarySize/3
)

// 01:-1 11:0 10:1
func para(in Trits) (*[curl.StateSize]uint64, *[curl.StateSize]uint64) {
	var l, h [curl.StateSize]uint64

	for i := 0; i < curl.StateSize; i++ {
		switch in[i] {
		case 0:
			l[i] = hBits
			h[i] = hBits
		case 1:
			l[i] = lBits
			h[i] = hBits
		case -1:
			l[i] = hBits
			h[i] = lBits
		}
	}
	return &l, &h
}

func incrN(n int, lmid *[curl.StateSize]uint64, hmid *[curl.StateSize]uint64) {
	for j := 0; j < n; j++ {
		var carry uint64 = 1

		// to avoid boundary check, I believe.
		for i := nonceInitStart; i < nonceIncrementStart && carry != 0; i++ {
			low := lmid[i]
			high := hmid[i]
			lmid[i] = high ^ low
			hmid[i] = low
			carry = high & (^low)
		}
	}
}

func transform64(lmid *[curl.StateSize]uint64, hmid *[curl.StateSize]uint64) {
	var ltmp, htmp [curl.StateSize]uint64
	lfrom := lmid
	hfrom := hmid
	lto := &ltmp
	hto := &htmp

	for r := 0; r < curl.NumberOfRounds-1; r++ {
		for j := 0; j < curl.StateSize; j++ {
			t1 := curl.Indices[j]
			t2 := curl.Indices[j+1]

			alpha := lfrom[t1]
			beta := hfrom[t1]
			gamma := hfrom[t2]
			delta := (alpha | (^gamma)) & (lfrom[t2] ^ beta)

			lto[j] = ^delta
			hto[j] = (alpha ^ gamma) | delta
		}

		lfrom, lto = lto, lfrom
		hfrom, hto = hto, hfrom
	}

	for j := 0; j < curl.StateSize; j++ {
		t1, t2 := curl.Indices[j], curl.Indices[j+1]

		alpha := lfrom[t1]
		beta := hfrom[t1]
		gamma := hfrom[t2]
		delta := (alpha | (^gamma)) & (lfrom[t2] ^ beta)

		lto[j] = ^delta
		hto[j] = (alpha ^ gamma) | delta
	}

	copy(lmid[:], ltmp[:])
	copy(hmid[:], htmp[:])
}

func incr(lmid *[curl.StateSize]uint64, hmid *[curl.StateSize]uint64) bool {
	var carry uint64 = 1
	var i int

	// to avoid boundary check, I believe.
	for i = nonceInitStart; i < HashTrinarySize && carry != 0; i++ {
		low := lmid[i]
		high := hmid[i]
		lmid[i] = high ^ low
		hmid[i] = low
		carry = high & (^low)
	}
	return i == HashTrinarySize
}

func seri(l *[curl.StateSize]uint64, h *[curl.StateSize]uint64, n uint) Trits {
	r := make(Trits, NonceTrinarySize)
	for i := nonceOffset; i < HashTrinarySize; i++ {
		ll := (l[i] >> n) & 1
		hh := (h[i] >> n) & 1

		switch {
		case hh == 0 && ll == 1:
			r[i-nonceOffset] = -1
		case hh == 1 && ll == 1:
			r[i-nonceOffset] = 0
		case hh == 1 && ll == 0:
			r[i-nonceOffset] = 1
		}
	}
	return r
}

func check(l *[curl.StateSize]uint64, h *[curl.StateSize]uint64, m int) int {
	nonceProbe := hBits
	for i := HashTrinarySize - m; i < HashTrinarySize; i++ {
		nonceProbe &= ^(l[i] ^ h[i])
		if nonceProbe == 0 {
			return -1
		}
	}

	var i uint
	for i = 0; i < 64; i++ {
		if (nonceProbe>>i)&1 == 1 {
			return int(i)
		}
	}
	return -1
}

func loop(lmid *[curl.StateSize]uint64, hmid *[curl.StateSize]uint64, m int, cancelled *bool) (Trits, int64) {
	var lcpy, hcpy [curl.StateSize]uint64
	var i int64
	for i = 0; !incr(lmid, hmid) && !*cancelled; i++ {
		copy(lcpy[:], lmid[:])
		copy(hcpy[:], hmid[:])
		transform64(&lcpy, &hcpy)

		if n := check(&lcpy, &hcpy, m); n >= 0 {
			nonce := seri(lmid, hmid, uint(n))
			return nonce, i * 64
		}
	}
	return nil, i * 64
}

// implementation of Proof-of-Work in Go
func goProofOfWork(trytes Trytes, mwm int, optRate chan int64, parallelism ...int) (Trytes, error) {
	if trytes == "" {
		return "", ErrInvalidTrytesForProofOfWork
	}

	// if any goroutine finds a nonce, then the cancel flag is set to true
	// and thereby all other ongoing Proof-of-Work tasks will halt.
	cancelled := false

	c := curl.NewCurl()
	c.Absorb(trytes[:(TransactionTrinarySize-HashTrinarySize)/3])
	tr := MustTrytesToTrits(trytes)
	copy(c.State, tr[TransactionTrinarySize-HashTrinarySize:])

	numGoroutines := proofOfWorkParallelism(parallelism...)
	var result Trytes
	var rate chan int64
	if optRate != nil {
		rate = make(chan int64, numGoroutines)
	}
	exit := make(chan struct{})
	nonceChan := make(chan Trytes)

	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			lmid, hmid := para(c.State)
			lmid[nonceOffset] = low0
			hmid[nonceOffset] = high0
			lmid[nonceOffset+1] = low1
			hmid[nonceOffset+1] = high1
			lmid[nonceOffset+2] = low2
			hmid[nonceOffset+2] = high2
			lmid[nonceOffset+3] = low3
			hmid[nonceOffset+3] = high3

			incrN(i, lmid, hmid)
			nonce, r := loop(lmid, hmid, mwm, &cancelled)

			if rate != nil {
				rate <- int64(math.Abs(float64(r)))
			}
			if r >= 0 && len(nonce) > 0 {
				select {
				case <-exit:
				case nonceChan <- MustTritsToTrytes(nonce):
					cancelled = true
				}
			}
		}(i)
	}

	if rate != nil {
		var rateSum int64
		for i := 0; i < numGoroutines; i++ {
			rateSum += <-rate
		}
		optRate <- rateSum
	}

	result = <-nonceChan
	close(exit)
	cancelled = true
	return result, nil
}

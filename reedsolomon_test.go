/**
 * Unit tests for ReedSolomon
 *
 * Copyright 2015, Klaus Post
 * Copyright 2015, Backblaze, Inc.  All rights reserved.
 */

package reedsolomon

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"
)

var noSSE2 = flag.Bool("no-sse2", !defaultOptions.useSSE2, "Disable SSE2")
var noSSSE3 = flag.Bool("no-ssse3", !defaultOptions.useSSSE3, "Disable SSSE3")
var noAVX2 = flag.Bool("no-avx2", !defaultOptions.useAVX2, "Disable AVX2")
var noAVX512 = flag.Bool("no-avx512", !defaultOptions.useAVX512, "Disable AVX512")
var noGNFI = flag.Bool("no-gfni", !defaultOptions.useAvx512GFNI, "Disable AVX512+GFNI")
var noAVX2GNFI = flag.Bool("no-avx-gfni", !defaultOptions.useAvxGNFI, "Disable AVX+GFNI")

func TestMain(m *testing.M) {
	flag.Parse()
	rs, _ := New(10, 3, testOptions()...)
	if rs != nil {
		if rst, ok := rs.(*reedSolomon); ok {
			fmt.Println("Using", rst.o.cpuOptions())
		}
	}
	os.Exit(m.Run())
}

func testOptions(o ...Option) []Option {
	o = append(o, WithFastOneParityMatrix())
	if *noSSSE3 {
		o = append(o, WithSSSE3(false))
	}
	if *noSSE2 {
		o = append(o, WithSSE2(false))
	}
	if *noAVX2 {
		o = append(o, WithAVX2(false))
	}
	if *noAVX512 {
		o = append(o, WithAVX512(false))
	}
	if *noGNFI {
		o = append(o, WithGFNI(false))
	}
	if *noAVX2GNFI {
		o = append(o, WithAVXGFNI(false))
	}
	return o
}

func isIncreasingAndContainsDataRow(indices []int) bool {
	cols := len(indices)
	for i := 0; i < cols-1; i++ {
		if indices[i] >= indices[i+1] {
			return false
		}
	}
	// Data rows are in the upper square portion of the matrix.
	return indices[0] < cols
}

func incrementIndices(indices []int, indexBound int) (valid bool) {
	for i := len(indices) - 1; i >= 0; i-- {
		indices[i]++
		if indices[i] < indexBound {
			break
		}

		if i == 0 {
			return false
		}

		indices[i] = 0
	}

	return true
}

func incrementIndicesUntilIncreasingAndContainsDataRow(
	indices []int, maxIndex int) bool {
	for {
		valid := incrementIndices(indices, maxIndex)
		if !valid {
			return false
		}

		if isIncreasingAndContainsDataRow(indices) {
			return true
		}
	}
}

func findSingularSubMatrix(m matrix) (matrix, error) {
	rows := len(m)
	cols := len(m[0])
	rowIndices := make([]int, cols)
	for incrementIndicesUntilIncreasingAndContainsDataRow(rowIndices, rows) {
		subMatrix, _ := newMatrix(cols, cols)
		for i, r := range rowIndices {
			for c := 0; c < cols; c++ {
				subMatrix[i][c] = m[r][c]
			}
		}

		_, err := subMatrix.Invert()
		if err == errSingular {
			return subMatrix, nil
		} else if err != nil {
			return nil, err
		}
	}

	return nil, nil
}

func TestBuildMatrixJerasure(t *testing.T) {
	totalShards := 12
	dataShards := 8
	m, err := buildMatrixJerasure(dataShards, totalShards)
	if err != nil {
		t.Fatal(err)
	}
	refMatrix := matrix{
		{1, 1, 1, 1, 1, 1, 1, 1},
		{1, 55, 39, 73, 84, 181, 225, 217},
		{1, 39, 217, 161, 92, 60, 172, 90},
		{1, 172, 70, 235, 143, 34, 200, 101},
	}
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			if i != j && m[i][j] != 0 || i == j && m[i][j] != 1 {
				t.Fatal("Top part of the matrix is not identity")
			}
		}
	}
	for i := 0; i < 4; i++ {
		for j := 0; j < 8; j++ {
			if m[8+i][j] != refMatrix[i][j] {
				t.Fatal("Coding matrix for EC 8+4 differs from Jerasure")
			}
		}
	}
}

func TestBuildMatrixPAR1Singular(t *testing.T) {
	totalShards := 8
	dataShards := 4
	m, err := buildMatrixPAR1(dataShards, totalShards)
	if err != nil {
		t.Fatal(err)
	}

	singularSubMatrix, err := findSingularSubMatrix(m)
	if err != nil {
		t.Fatal(err)
	}

	if singularSubMatrix == nil {
		t.Fatal("No singular sub-matrix found")
	}

	t.Logf("matrix %s has singular sub-matrix %s", m, singularSubMatrix)
}

func testOpts() [][]Option {
	if testing.Short() {
		return [][]Option{
			{WithCauchyMatrix()}, {WithLeopardGF16(true)}, {WithLeopardGF(true)},
		}
	}
	opts := [][]Option{
		{WithPAR1Matrix()}, {WithCauchyMatrix()},
		{WithFastOneParityMatrix()}, {WithPAR1Matrix(), WithFastOneParityMatrix()}, {WithCauchyMatrix(), WithFastOneParityMatrix()},
		{WithMaxGoroutines(1), WithMinSplitSize(500), WithSSSE3(false), WithAVX2(false), WithAVX512(false)},
		{WithMaxGoroutines(5000), WithMinSplitSize(50), WithSSSE3(false), WithAVX2(false), WithAVX512(false)},
		{WithMaxGoroutines(5000), WithMinSplitSize(500000), WithSSSE3(false), WithAVX2(false), WithAVX512(false)},
		{WithMaxGoroutines(1), WithMinSplitSize(500000), WithSSSE3(false), WithAVX2(false), WithAVX512(false)},
		{WithAutoGoroutines(50000), WithMinSplitSize(500)},
		{WithInversionCache(false)},
		{WithJerasureMatrix()},
		{WithLeopardGF16(true)},
		{WithLeopardGF(true)},
	}

	for _, o := range opts[:] {
		if defaultOptions.useSSSE3 {
			n := make([]Option, len(o), len(o)+1)
			copy(n, o)
			n = append(n, WithSSSE3(true))
			opts = append(opts, n)
		}
		if defaultOptions.useAVX2 {
			n := make([]Option, len(o), len(o)+1)
			copy(n, o)
			n = append(n, WithAVX2(true))
			opts = append(opts, n)
		}
		if defaultOptions.useAVX512 {
			n := make([]Option, len(o), len(o)+1)
			copy(n, o)
			n = append(n, WithAVX512(true))
			opts = append(opts, n)
		}
		if defaultOptions.useAvx512GFNI {
			n := make([]Option, len(o), len(o)+1)
			copy(n, o)
			n = append(n, WithGFNI(false))
			opts = append(opts, n)
		}
	}
	return opts
}

func parallelIfNotShort(t *testing.T) {
	if !testing.Short() {
		t.Parallel()
	}
}

func TestEncoding(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		parallelIfNotShort(t)
		testEncoding(t, testOptions()...)
	})
	t.Run("default-idx", func(t *testing.T) {
		parallelIfNotShort(t)
		testEncodingIdx(t, testOptions()...)
	})
	if testing.Short() {
		return
	}
	// Spread somewhat, but don't overload...
	to := testOpts()
	to2 := to[len(to)/2:]
	to = to[:len(to)/2]
	t.Run("reg", func(t *testing.T) {
		parallelIfNotShort(t)
		for i, o := range to {
			t.Run(fmt.Sprintf("opt-%d", i), func(t *testing.T) {
				testEncoding(t, o...)
			})
		}
	})
	t.Run("reg2", func(t *testing.T) {
		parallelIfNotShort(t)
		for i, o := range to2 {
			t.Run(fmt.Sprintf("opt-%d", i), func(t *testing.T) {
				testEncoding(t, o...)
			})
		}
	})
	if !testing.Short() {
		t.Run("idx", func(t *testing.T) {
			parallelIfNotShort(t)
			for i, o := range to {
				t.Run(fmt.Sprintf("idx-opt-%d", i), func(t *testing.T) {
					testEncodingIdx(t, o...)
				})
			}
		})
		t.Run("idx2", func(t *testing.T) {
			parallelIfNotShort(t)
			for i, o := range to2 {
				t.Run(fmt.Sprintf("idx-opt-%d", i), func(t *testing.T) {
					testEncodingIdx(t, o...)
				})
			}
		})

	}
}

// matrix sizes to test.
// note that par1 matrix will fail on some combinations.
func testSizes() [][2]int {
	if testing.Short() {
		return [][2]int{
			{3, 0},
			{1, 1}, {1, 2}, {8, 4}, {10, 30}, {41, 17},
			{256, 20}, {500, 300},
		}
	}
	return [][2]int{
		{1, 0}, {10, 0}, {12, 0}, {49, 0},
		{1, 1}, {1, 2}, {3, 3}, {3, 1}, {5, 3}, {8, 4}, {10, 30}, {12, 10}, {14, 7}, {41, 17}, {49, 1}, {5, 20},
		{256, 20}, {500, 300}, {2945, 129},
	}
}

var testDataSizes = []int{10, 100, 1000, 10001, 100003, 1000055}
var testDataSizesShort = []int{10, 10001, 100003}

func testEncoding(t *testing.T, o ...Option) {
	for _, size := range testSizes() {
		data, parity := size[0], size[1]
		rng := rand.New(rand.NewSource(0xabadc0cac01a))
		t.Run(fmt.Sprintf("%dx%d", data, parity), func(t *testing.T) {
			sz := testDataSizes
			if testing.Short() || data+parity > 256 {
				sz = testDataSizesShort
				if raceEnabled {
					sz = testDataSizesShort[:1]
				}
			}
			for _, perShard := range sz {
				r, err := New(data, parity, testOptions(o...)...)
				if err != nil {
					t.Fatal(err)
				}
				x := r.(Extensions)
				if want, got := data, x.DataShards(); want != got {
					t.Errorf("DataShards returned %d, want %d", got, want)
				}
				if want, got := parity, x.ParityShards(); want != got {
					t.Errorf("ParityShards returned %d, want %d", got, want)
				}
				if want, got := parity+data, x.TotalShards(); want != got {
					t.Errorf("TotalShards returned %d, want %d", got, want)
				}
				mul := x.ShardSizeMultiple()
				if mul <= 0 {
					t.Fatalf("Got unexpected ShardSizeMultiple: %d", mul)
				}
				perShard = ((perShard + mul - 1) / mul) * mul

				t.Run(fmt.Sprint(perShard), func(t *testing.T) {

					shards := make([][]byte, data+parity)
					for s := range shards {
						shards[s] = make([]byte, perShard)
					}

					for s := 0; s < len(shards); s++ {
						rng.Read(shards[s])
					}

					err = r.Encode(shards)
					if err != nil {
						t.Fatal(err)
					}
					ok, err := r.Verify(shards)
					if err != nil {
						t.Fatal(err)
					}
					if !ok {
						t.Fatal("Verification failed")
					}

					if parity == 0 {
						// Check that Reconstruct and ReconstructData do nothing
						err = r.ReconstructData(shards)
						if err != nil {
							t.Fatal(err)
						}
						err = r.Reconstruct(shards)
						if err != nil {
							t.Fatal(err)
						}

						// Skip integrity checks
						return
					}

					// Delete one in data
					idx := rng.Intn(data)
					want := shards[idx]
					shards[idx] = nil

					err = r.ReconstructData(shards)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(shards[idx], want) {
						t.Fatal("did not ReconstructData correctly")
					}

					// Delete one randomly
					idx = rng.Intn(data + parity)
					want = shards[idx]
					shards[idx] = nil
					err = r.Reconstruct(shards)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(shards[idx], want) {
						t.Fatal("did not Reconstruct correctly")
					}

					err = r.Encode(make([][]byte, 1))
					if err != ErrTooFewShards {
						t.Errorf("expected %v, got %v", ErrTooFewShards, err)
					}

					// Make one too short.
					shards[idx] = shards[idx][:perShard-1]
					err = r.Encode(shards)
					if err != ErrShardSize {
						t.Errorf("expected %v, got %v", ErrShardSize, err)
					}
				})
			}
		})

	}
}

func testEncodingIdx(t *testing.T, o ...Option) {
	for _, size := range testSizes() {
		data, parity := size[0], size[1]
		rng := rand.New(rand.NewSource(0xabadc0cac01a))
		t.Run(fmt.Sprintf("%dx%d", data, parity), func(t *testing.T) {

			sz := testDataSizes
			if testing.Short() {
				sz = testDataSizesShort
			}
			for _, perShard := range sz {
				r, err := New(data, parity, testOptions(o...)...)
				if err != nil {
					t.Fatal(err)
				}
				if err := r.EncodeIdx(nil, 0, nil); err == ErrNotSupported {
					t.Skip(err)
					return
				}
				mul := r.(Extensions).ShardSizeMultiple()
				perShard = ((perShard + mul - 1) / mul) * mul

				t.Run(fmt.Sprint(perShard), func(t *testing.T) {

					shards := AllocAligned(data+parity, perShard)
					shuffle := make([]int, data)
					for i := range shuffle {
						shuffle[i] = i
					}
					rng.Shuffle(len(shuffle), func(i, j int) { shuffle[i], shuffle[j] = shuffle[j], shuffle[i] })

					// Send shards in random order.
					for s := 0; s < data; s++ {
						s := shuffle[s]
						rng.Read(shards[s])
						err = r.EncodeIdx(shards[s], s, shards[data:])
						if err != nil {
							t.Fatal(err)
						}
					}

					ok, err := r.Verify(shards)
					if err != nil {
						t.Fatal(err)
					}
					if !ok {
						t.Fatal("Verification failed")
					}

					if parity == 0 {
						// Check that Reconstruct and ReconstructData do nothing
						err = r.ReconstructData(shards)
						if err != nil {
							t.Fatal(err)
						}
						err = r.Reconstruct(shards)
						if err != nil {
							t.Fatal(err)
						}

						// Skip integrity checks
						return
					}

					// Delete one in data
					idx := rng.Intn(data)
					want := shards[idx]
					shards[idx] = nil

					err = r.ReconstructData(shards)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(shards[idx], want) {
						t.Fatal("did not ReconstructData correctly")
					}

					// Delete one randomly
					idx = rng.Intn(data + parity)
					want = shards[idx]
					shards[idx] = nil
					err = r.Reconstruct(shards)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(shards[idx], want) {
						t.Fatal("did not Reconstruct correctly")
					}

					err = r.Encode(make([][]byte, 1))
					if err != ErrTooFewShards {
						t.Errorf("expected %v, got %v", ErrTooFewShards, err)
					}

					// Make one too short.
					shards[idx] = shards[idx][:perShard-1]
					err = r.Encode(shards)
					if err != ErrShardSize {
						t.Errorf("expected %v, got %v", ErrShardSize, err)
					}
				})
			}
		})

	}
}

func TestUpdate(t *testing.T) {
	parallelIfNotShort(t)
	for i, o := range testOpts() {
		t.Run(fmt.Sprintf("options %d", i), func(t *testing.T) {
			testUpdate(t, o...)
		})
	}
}

func testUpdate(t *testing.T, o ...Option) {
	for _, size := range [][2]int{{10, 3}, {17, 2}} {
		data, parity := size[0], size[1]
		t.Run(fmt.Sprintf("%dx%d", data, parity), func(t *testing.T) {
			sz := testDataSizesShort
			if testing.Short() {
				sz = []int{50000}
			}
			for _, perShard := range sz {
				r, err := New(data, parity, testOptions(o...)...)
				if err != nil {
					t.Fatal(err)
				}
				mul := r.(Extensions).ShardSizeMultiple()
				perShard = ((perShard + mul - 1) / mul) * mul

				t.Run(fmt.Sprint(perShard), func(t *testing.T) {

					shards := make([][]byte, data+parity)
					for s := range shards {
						shards[s] = make([]byte, perShard)
					}

					for s := range shards {
						fillRandom(shards[s])
					}

					err = r.Encode(shards)
					if err != nil {
						t.Fatal(err)
					}
					ok, err := r.Verify(shards)
					if err != nil {
						t.Fatal(err)
					}
					if !ok {
						t.Fatal("Verification failed")
					}

					newdatashards := make([][]byte, data)
					for s := range newdatashards {
						newdatashards[s] = make([]byte, perShard)
						fillRandom(newdatashards[s])
						err = r.Update(shards, newdatashards)
						if err != nil {
							if errors.Is(err, ErrNotSupported) {
								t.Skip(err)
								return
							}
							t.Fatal(err)
						}
						shards[s] = newdatashards[s]
						ok, err := r.Verify(shards)
						if err != nil {
							t.Fatal(err)
						}
						if !ok {
							t.Fatal("Verification failed")
						}
						newdatashards[s] = nil
					}
					for s := 0; s < len(newdatashards)-1; s++ {
						newdatashards[s] = make([]byte, perShard)
						newdatashards[s+1] = make([]byte, perShard)
						fillRandom(newdatashards[s])
						fillRandom(newdatashards[s+1])
						err = r.Update(shards, newdatashards)
						if err != nil {
							t.Fatal(err)
						}
						shards[s] = newdatashards[s]
						shards[s+1] = newdatashards[s+1]
						ok, err := r.Verify(shards)
						if err != nil {
							t.Fatal(err)
						}
						if !ok {
							t.Fatal("Verification failed")
						}
						newdatashards[s] = nil
						newdatashards[s+1] = nil
					}
					for newNum := 1; newNum <= data; newNum++ {
						for s := 0; s <= data-newNum; s++ {
							for i := 0; i < newNum; i++ {
								newdatashards[s+i] = make([]byte, perShard)
								fillRandom(newdatashards[s+i])
							}
							err = r.Update(shards, newdatashards)
							if err != nil {
								t.Fatal(err)
							}
							for i := 0; i < newNum; i++ {
								shards[s+i] = newdatashards[s+i]
							}
							ok, err := r.Verify(shards)
							if err != nil {
								t.Fatal(err)
							}
							if !ok {
								t.Fatal("Verification failed")
							}
							for i := 0; i < newNum; i++ {
								newdatashards[s+i] = nil
							}
						}
					}
				})
			}
		})
	}
}

func TestReconstruct(t *testing.T) {
	parallelIfNotShort(t)
	testReconstruct(t)
	for i, o := range testOpts() {
		t.Run(fmt.Sprintf("options %d", i), func(t *testing.T) {
			testReconstruct(t, o...)
		})
	}
}

func testReconstruct(t *testing.T, o ...Option) {
	perShard := 50000
	r, err := New(10, 3, testOptions(o...)...)
	if err != nil {
		t.Fatal(err)
	}
	xt := r.(Extensions)
	mul := xt.ShardSizeMultiple()
	perShard = ((perShard + mul - 1) / mul) * mul

	t.Log(perShard)
	shards := make([][]byte, 13)
	for s := range shards {
		shards[s] = make([]byte, perShard)
	}

	for s := 0; s < 13; s++ {
		fillRandom(shards[s])
	}

	err = r.Encode(shards)
	if err != nil {
		t.Fatal(err)
	}

	// Reconstruct with all shards present
	err = r.Reconstruct(shards)
	if err != nil {
		t.Fatal(err)
	}

	// Reconstruct with 10 shards present. Use pre-allocated memory for one of them.
	shards[0] = nil
	shards[7] = nil
	shard11 := shards[11]
	shards[11] = shard11[:0]
	fillRandom(shard11)

	err = r.Reconstruct(shards)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := r.Verify(shards)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Verification failed")
	}

	if &shard11[0] != &shards[11][0] {
		t.Errorf("Shard was not reconstructed into pre-allocated memory")
	}

	// Reconstruct with 9 shards present (should fail)
	shards[0] = nil
	shards[4] = nil
	shards[7] = nil
	shards[11] = nil

	err = r.Reconstruct(shards)
	if err != ErrTooFewShards {
		t.Errorf("expected %v, got %v", ErrTooFewShards, err)
	}

	err = r.Reconstruct(make([][]byte, 1))
	if err != ErrTooFewShards {
		t.Errorf("expected %v, got %v", ErrTooFewShards, err)
	}
	err = r.Reconstruct(make([][]byte, 13))
	if err != ErrShardNoData {
		t.Errorf("expected %v, got %v", ErrShardNoData, err)
	}
}

func TestReconstructCustom(t *testing.T) {
	perShard := 50000
	r, err := New(4, 3, WithCustomMatrix([][]byte{
		{1, 1, 0, 0},
		{0, 0, 1, 1},
		{1, 2, 3, 4},
	}))
	if err != nil {
		t.Fatal(err)
	}
	shards := make([][]byte, 7)
	for s := range shards {
		shards[s] = make([]byte, perShard)
	}

	for s := 0; s < len(shards); s++ {
		fillRandom(shards[s])
	}

	err = r.Encode(shards)
	if err != nil {
		t.Fatal(err)
	}

	// Reconstruct with 1 shard absent.
	shards1 := make([][]byte, len(shards))
	copy(shards1, shards)
	shards1[0] = nil

	err = r.Reconstruct(shards1)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := r.Verify(shards)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Verification failed")
	}

	// Reconstruct with 3 shards absent.
	copy(shards1, shards)
	shards1[0] = nil
	shards1[1] = nil
	shards1[2] = nil

	err = r.Reconstruct(shards1)
	if err != nil {
		t.Fatal(err)
	}

	ok, err = r.Verify(shards)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Verification failed")
	}
}

func TestReconstructData(t *testing.T) {
	parallelIfNotShort(t)
	testReconstructData(t)
	for i, o := range testOpts() {
		t.Run(fmt.Sprintf("options %d", i), func(t *testing.T) {
			testReconstructData(t, o...)
		})
	}
}

func testReconstructData(t *testing.T, o ...Option) {
	perShard := 100000
	r, err := New(8, 5, testOptions(o...)...)
	if err != nil {
		t.Fatal(err)
	}
	mul := r.(Extensions).ShardSizeMultiple()
	perShard = ((perShard + mul - 1) / mul) * mul

	shards := make([][]byte, 13)
	for s := range shards {
		shards[s] = make([]byte, perShard)
	}

	for s := 0; s < 13; s++ {
		fillRandom(shards[s], int64(s))
	}

	err = r.Encode(shards)
	if err != nil {
		t.Fatal(err)
	}

	// Reconstruct with all shards present
	err = r.ReconstructData(shards)
	if err != nil {
		t.Fatal(err)
	}

	// Reconstruct parity shards from data
	shardsCopy := make([][]byte, 13)
	for i := 0; i < 8; i++ {
		shardsCopy[i] = shards[i]
	}

	shardsRequired := make([]bool, 13)
	shardsRequired[10] = true

	err = r.ReconstructSome(shardsCopy, shardsRequired)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(shardsCopy[10], shards[10]) {
		t.Fatal("ReconstructSome did not reconstruct required shards correctly")
	}

	// Reconstruct 3 shards with 3 data and 5 parity shards
	shardsCopy = make([][]byte, 13)
	copy(shardsCopy, shards)
	shardsCopy[2] = nil
	shardsCopy[3] = nil
	shardsCopy[4] = nil
	shardsCopy[5] = nil
	shardsCopy[6] = nil

	shardsRequired = make([]bool, 8)
	shardsRequired[3] = true
	shardsRequired[4] = true
	err = r.ReconstructSome(shardsCopy, shardsRequired)
	if err != nil {
		t.Fatal(err)
	}

	if 0 != bytes.Compare(shardsCopy[3], shards[3]) ||
		0 != bytes.Compare(shardsCopy[4], shards[4]) {
		t.Fatal("ReconstructSome did not reconstruct required shards correctly")
	}

	if shardsCopy[2] != nil || shardsCopy[5] != nil || shardsCopy[6] != nil {
		// This is expected in some cases.
		t.Log("ReconstructSome reconstructed extra shards")
	}

	// Reconstruct with 10 shards present. Use pre-allocated memory for one of them.
	shards[0] = nil
	shards[2] = nil
	shard4 := shards[4]
	shards[4] = shard4[:0]
	fillRandom(shard4, 4)

	err = r.ReconstructData(shards)
	if err != nil {
		t.Fatal(err)
	}

	// Since all parity shards are available, verification will succeed
	ok, err := r.Verify(shards)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Verification failed")
	}

	if &shard4[0] != &shards[4][0] {
		t.Errorf("Shard was not reconstructed into pre-allocated memory")
	}

	// Reconstruct with 6 data and 4 parity shards
	shards[0] = nil
	shards[2] = nil
	shards[12] = nil

	err = r.ReconstructData(shards)
	if err != nil {
		t.Fatal(err)
	}

	// Verification will fail now due to absence of a parity block
	_, err = r.Verify(shards)
	if err != ErrShardSize {
		t.Errorf("expected %v, got %v", ErrTooFewShards, err)
	}

	// Reconstruct with 7 data and 1 parity shards
	shards[0] = nil
	shards[9] = nil
	shards[10] = nil
	shards[11] = nil
	shards[12] = nil

	err = r.ReconstructData(shards)
	if err != nil {
		t.Fatal(err)
	}

	_, err = r.Verify(shards)
	if err != ErrShardSize {
		t.Errorf("expected %v, got %v", ErrTooFewShards, err)
	}

	// Reconstruct with 6 data and 1 parity shards (should fail)
	shards[0] = nil
	shards[1] = nil
	shards[9] = nil
	shards[10] = nil
	shards[11] = nil
	shards[12] = nil

	err = r.ReconstructData(shards)
	if err != ErrTooFewShards {
		t.Errorf("expected %v, got %v", ErrTooFewShards, err)
	}

	err = r.ReconstructData(make([][]byte, 1))
	if err != ErrTooFewShards {
		t.Errorf("expected %v, got %v", ErrTooFewShards, err)
	}
	err = r.ReconstructData(make([][]byte, 13))
	if err != ErrShardNoData {
		t.Errorf("expected %v, got %v", ErrShardNoData, err)
	}
}

func TestReconstructPAR1Singular(t *testing.T) {
	parallelIfNotShort(t)
	perShard := 50
	r, err := New(4, 4, testOptions(WithPAR1Matrix())...)
	if err != nil {
		t.Fatal(err)
	}
	shards := make([][]byte, 8)
	for s := range shards {
		shards[s] = make([]byte, perShard)
	}

	for s := 0; s < 8; s++ {
		fillRandom(shards[s])
	}

	err = r.Encode(shards)
	if err != nil {
		t.Fatal(err)
	}

	// Reconstruct with only the last data shard present, and the
	// first, second, and fourth parity shard present (based on
	// the result of TestBuildMatrixPAR1Singular). This should
	// fail.
	shards[0] = nil
	shards[1] = nil
	shards[2] = nil
	shards[6] = nil

	err = r.Reconstruct(shards)
	if err != errSingular {
		t.Fatal(err)
		t.Errorf("expected %v, got %v", errSingular, err)
	}
}

func TestVerify(t *testing.T) {
	parallelIfNotShort(t)
	testVerify(t)
	for i, o := range testOpts() {
		t.Run(fmt.Sprintf("options %d", i), func(t *testing.T) {
			testVerify(t, o...)
		})
	}
}

func testVerify(t *testing.T, o ...Option) {
	perShard := 33333
	r, err := New(10, 4, testOptions(o...)...)
	if err != nil {
		t.Fatal(err)
	}
	mul := r.(Extensions).ShardSizeMultiple()
	perShard = ((perShard + mul - 1) / mul) * mul

	shards := make([][]byte, 14)
	for s := range shards {
		shards[s] = make([]byte, perShard)
	}

	for s := 0; s < 10; s++ {
		fillRandom(shards[s], 0)
	}

	err = r.Encode(shards)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := r.Verify(shards)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("Verification failed")
		return
	}

	// Put in random data. Verification should fail
	fillRandom(shards[10], 1)
	ok, err = r.Verify(shards)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("Verification did not fail")
	}
	// Re-encode
	err = r.Encode(shards)
	if err != nil {
		t.Fatal(err)
	}
	// Fill a data segment with random data
	fillRandom(shards[0], 2)
	ok, err = r.Verify(shards)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("Verification did not fail")
	}

	_, err = r.Verify(make([][]byte, 1))
	if err != ErrTooFewShards {
		t.Errorf("expected %v, got %v", ErrTooFewShards, err)
	}

	_, err = r.Verify(make([][]byte, 14))
	if err != ErrShardNoData {
		t.Errorf("expected %v, got %v", ErrShardNoData, err)
	}
}

func TestOneEncode(t *testing.T) {
	codec, err := New(5, 5, testOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	shards := [][]byte{
		{0, 1},
		{4, 5},
		{2, 3},
		{6, 7},
		{8, 9},
		{0, 0},
		{0, 0},
		{0, 0},
		{0, 0},
		{0, 0},
	}
	codec.Encode(shards)
	if shards[5][0] != 12 || shards[5][1] != 13 {
		t.Fatal("shard 5 mismatch")
	}
	if shards[6][0] != 10 || shards[6][1] != 11 {
		t.Fatal("shard 6 mismatch")
	}
	if shards[7][0] != 14 || shards[7][1] != 15 {
		t.Fatal("shard 7 mismatch")
	}
	if shards[8][0] != 90 || shards[8][1] != 91 {
		t.Fatal("shard 8 mismatch")
	}
	if shards[9][0] != 94 || shards[9][1] != 95 {
		t.Fatal("shard 9 mismatch")
	}

	ok, err := codec.Verify(shards)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("did not verify")
	}
	shards[8][0]++
	ok, err = codec.Verify(shards)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("verify did not fail as expected")
	}

}

func fillRandom(p []byte, seed ...int64) {
	src := rand.NewSource(time.Now().UnixNano())
	if len(seed) > 0 {
		src = rand.NewSource(seed[0])
	}
	rng := rand.New(src)
	for i := 0; i < len(p); i += 7 {
		val := rng.Int63()
		for j := 0; i+j < len(p) && j < 7; j++ {
			p[i+j] = byte(val)
			val >>= 8
		}
	}
}

func benchmarkEncode(b *testing.B, dataShards, parityShards, shardSize int, opts ...Option) {
	opts = append(testOptions(WithAutoGoroutines(shardSize)), opts...)
	r, err := New(dataShards, parityShards, opts...)
	if err != nil {
		b.Fatal(err)
	}

	shards := r.(Extensions).AllocAligned(shardSize)
	for s := 0; s < dataShards; s++ {
		fillRandom(shards[s])
	}
	// Warm up so initialization is eliminated.
	err = r.Encode(shards)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(shardSize * (dataShards + parityShards)))
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		err = r.Encode(shards)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkDecode(b *testing.B, dataShards, parityShards, shardSize, deleteShards int, opts ...Option) {
	opts = append(testOptions(WithAutoGoroutines(shardSize)), opts...)
	r, err := New(dataShards, parityShards, opts...)
	if err != nil {
		b.Fatal(err)
	}

	shards := r.(Extensions).AllocAligned(shardSize)
	for s := 0; s < dataShards; s++ {
		fillRandom(shards[s])
	}
	if err := r.Encode(shards); err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(shardSize * (dataShards + parityShards)))
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Clear maximum number of data shards.
		for s := 0; s < deleteShards; s++ {
			shards[s] = nil
		}

		err = r.Reconstruct(shards)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode2x1x1M(b *testing.B) {
	benchmarkEncode(b, 2, 1, 1024*1024)
}

// Benchmark 800 data slices with 200 parity slices
func BenchmarkEncode800x200(b *testing.B) {
	for size := 64; size <= 1<<20; size *= 4 {
		b.Run(fmt.Sprintf("%v", size), func(b *testing.B) {
			benchmarkEncode(b, 800, 200, size)
		})
	}
}

// Benchmark 1K encode with symmetric shard sizes.
func BenchmarkEncode1K(b *testing.B) {
	for shards := 4; shards < 65536; shards *= 2 {
		b.Run(fmt.Sprintf("%v+%v", shards, shards), func(b *testing.B) {
			if shards*2 <= 256 {
				b.Run(fmt.Sprint("cauchy"), func(b *testing.B) {
					benchmarkEncode(b, shards, shards, 1024, WithCauchyMatrix())
				})
				b.Run(fmt.Sprint("leopard-gf8"), func(b *testing.B) {
					benchmarkEncode(b, shards, shards, 1024, WithLeopardGF(true))
				})
			}
			b.Run(fmt.Sprint("leopard-gf16"), func(b *testing.B) {
				benchmarkEncode(b, shards, shards, 1024, WithLeopardGF16(true))
			})
		})
	}
}

// Benchmark 1K decode with symmetric shard sizes.
func BenchmarkDecode1K(b *testing.B) {
	for shards := 4; shards < 65536; shards *= 2 {
		b.Run(fmt.Sprintf("%v+%v", shards, shards), func(b *testing.B) {
			if shards*2 <= 256 {
				b.Run(fmt.Sprint("cauchy"), func(b *testing.B) {
					benchmarkDecode(b, shards, shards, 1024, shards, WithCauchyMatrix(), WithInversionCache(false))
				})
				b.Run(fmt.Sprint("cauchy-inv"), func(b *testing.B) {
					benchmarkDecode(b, shards, shards, 1024, shards, WithCauchyMatrix(), WithInversionCache(true))
				})
				b.Run(fmt.Sprint("cauchy-single"), func(b *testing.B) {
					benchmarkDecode(b, shards, shards, 1024, 1, WithCauchyMatrix(), WithInversionCache(false))
				})
				b.Run(fmt.Sprint("cauchy-single-inv"), func(b *testing.B) {
					benchmarkDecode(b, shards, shards, 1024, 1, WithCauchyMatrix(), WithInversionCache(true))
				})
				b.Run(fmt.Sprint("leopard-gf8"), func(b *testing.B) {
					benchmarkDecode(b, shards, shards, 1024, shards, WithLeopardGF(true), WithInversionCache(false))
				})
				b.Run(fmt.Sprint("leopard-gf8-inv"), func(b *testing.B) {
					benchmarkDecode(b, shards, shards, 1024, shards, WithLeopardGF(true), WithInversionCache(true))
				})
				b.Run(fmt.Sprint("leopard-gf8-single"), func(b *testing.B) {
					benchmarkDecode(b, shards, shards, 1024, 1, WithLeopardGF(true), WithInversionCache(false))
				})
				b.Run(fmt.Sprint("leopard-gf8-single-inv"), func(b *testing.B) {
					benchmarkDecode(b, shards, shards, 1024, 1, WithLeopardGF(true), WithInversionCache(true))
				})
			}
			b.Run(fmt.Sprint("leopard-gf16"), func(b *testing.B) {
				benchmarkDecode(b, shards, shards, 1024, shards, WithLeopardGF16(true))
			})
			b.Run(fmt.Sprint("leopard-gf16-single"), func(b *testing.B) {
				benchmarkDecode(b, shards, shards, 1024, 1, WithLeopardGF16(true))
			})
		})
	}
}

func BenchmarkEncodeLeopard(b *testing.B) {
	size := (64 << 20) / 800 / 64 * 64
	b.Run(strconv.Itoa(size), func(b *testing.B) {
		benchmarkEncode(b, 800, 200, size)
	})
}

func BenchmarkEncode10x2x10000(b *testing.B) {
	benchmarkEncode(b, 10, 2, 10000)
}

func BenchmarkEncode100x20x10000(b *testing.B) {
	benchmarkEncode(b, 100, 20, 10000)
}

func BenchmarkEncode17x3x1M(b *testing.B) {
	benchmarkEncode(b, 17, 3, 1024*1024)
}

// Benchmark 10 data shards and 4 parity shards with 16MB each.
func BenchmarkEncode10x4x16M(b *testing.B) {
	benchmarkEncode(b, 10, 4, 16*1024*1024)
}

// Benchmark 5 data shards and 2 parity shards with 1MB each.
func BenchmarkEncode5x2x1M(b *testing.B) {
	benchmarkEncode(b, 5, 2, 1024*1024)
}

// Benchmark 1 data shards and 2 parity shards with 1MB each.
func BenchmarkEncode10x2x1M(b *testing.B) {
	benchmarkEncode(b, 10, 2, 1024*1024)
}

// Benchmark 10 data shards and 4 parity shards with 1MB each.
func BenchmarkEncode10x4x1M(b *testing.B) {
	benchmarkEncode(b, 10, 4, 1024*1024)
}

// Benchmark 50 data shards and 20 parity shards with 1M each.
func BenchmarkEncode50x20x1M(b *testing.B) {
	benchmarkEncode(b, 50, 20, 1024*1024)
}

// Benchmark 50 data shards and 20 parity shards with 1M each.
func BenchmarkEncodeLeopard50x20x1M(b *testing.B) {
	benchmarkEncode(b, 50, 20, 1024*1024, WithLeopardGF(true))
}

// Benchmark 17 data shards and 3 parity shards with 16MB each.
func BenchmarkEncode17x3x16M(b *testing.B) {
	benchmarkEncode(b, 17, 3, 16*1024*1024)
}

func BenchmarkEncode_8x4x8M(b *testing.B)   { benchmarkEncode(b, 8, 4, 8*1024*1024) }
func BenchmarkEncode_12x4x12M(b *testing.B) { benchmarkEncode(b, 12, 4, 12*1024*1024) }
func BenchmarkEncode_16x4x16M(b *testing.B) { benchmarkEncode(b, 16, 4, 16*1024*1024) }
func BenchmarkEncode_16x4x32M(b *testing.B) { benchmarkEncode(b, 16, 4, 32*1024*1024) }
func BenchmarkEncode_16x4x64M(b *testing.B) { benchmarkEncode(b, 16, 4, 64*1024*1024) }

func BenchmarkEncode_8x5x8M(b *testing.B)  { benchmarkEncode(b, 8, 5, 8*1024*1024) }
func BenchmarkEncode_8x6x8M(b *testing.B)  { benchmarkEncode(b, 8, 6, 8*1024*1024) }
func BenchmarkEncode_8x7x8M(b *testing.B)  { benchmarkEncode(b, 8, 7, 8*1024*1024) }
func BenchmarkEncode_8x9x8M(b *testing.B)  { benchmarkEncode(b, 8, 9, 8*1024*1024) }
func BenchmarkEncode_8x10x8M(b *testing.B) { benchmarkEncode(b, 8, 10, 8*1024*1024) }
func BenchmarkEncode_8x11x8M(b *testing.B) { benchmarkEncode(b, 8, 11, 8*1024*1024) }

func BenchmarkEncode_8x8x05M(b *testing.B) { benchmarkEncode(b, 8, 8, 1*1024*1024/2) }
func BenchmarkEncode_8x8x1M(b *testing.B)  { benchmarkEncode(b, 8, 8, 1*1024*1024) }
func BenchmarkEncode_8x8x8M(b *testing.B)  { benchmarkEncode(b, 8, 8, 8*1024*1024) }
func BenchmarkEncode_8x8x32M(b *testing.B) { benchmarkEncode(b, 8, 8, 32*1024*1024) }

func BenchmarkEncode_24x8x24M(b *testing.B) { benchmarkEncode(b, 24, 8, 24*1024*1024) }
func BenchmarkEncode_24x8x48M(b *testing.B) { benchmarkEncode(b, 24, 8, 48*1024*1024) }

func benchmarkVerify(b *testing.B, dataShards, parityShards, shardSize int) {
	r, err := New(dataShards, parityShards, testOptions(WithAutoGoroutines(shardSize))...)
	if err != nil {
		b.Fatal(err)
	}
	shards := r.(Extensions).AllocAligned(shardSize)

	for s := 0; s < dataShards; s++ {
		fillRandom(shards[s])
	}
	err = r.Encode(shards)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(shardSize * (dataShards + parityShards)))
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err = r.Verify(shards)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark 800 data slices with 200 parity slices
func BenchmarkVerify800x200(b *testing.B) {
	for size := 64; size <= 1<<20; size *= 4 {
		b.Run(fmt.Sprintf("%v", size), func(b *testing.B) {
			benchmarkVerify(b, 800, 200, size)
		})
	}
}

// Benchmark 10 data slices with 2 parity slices holding 10000 bytes each
func BenchmarkVerify10x2x10000(b *testing.B) {
	benchmarkVerify(b, 10, 2, 10000)
}

// Benchmark 50 data slices with 5 parity slices holding 100000 bytes each
func BenchmarkVerify50x5x100000(b *testing.B) {
	benchmarkVerify(b, 50, 5, 100000)
}

// Benchmark 10 data slices with 2 parity slices holding 1MB bytes each
func BenchmarkVerify10x2x1M(b *testing.B) {
	benchmarkVerify(b, 10, 2, 1024*1024)
}

// Benchmark 5 data slices with 2 parity slices holding 1MB bytes each
func BenchmarkVerify5x2x1M(b *testing.B) {
	benchmarkVerify(b, 5, 2, 1024*1024)
}

// Benchmark 10 data slices with 4 parity slices holding 1MB bytes each
func BenchmarkVerify10x4x1M(b *testing.B) {
	benchmarkVerify(b, 10, 4, 1024*1024)
}

// Benchmark 5 data slices with 2 parity slices holding 1MB bytes each
func BenchmarkVerify50x20x1M(b *testing.B) {
	benchmarkVerify(b, 50, 20, 1024*1024)
}

// Benchmark 10 data slices with 4 parity slices holding 16MB bytes each
func BenchmarkVerify10x4x16M(b *testing.B) {
	benchmarkVerify(b, 10, 4, 16*1024*1024)
}

func corruptRandom(shards [][]byte, dataShards, parityShards int) {
	shardsToCorrupt := rand.Intn(parityShards) + 1
	for i := 0; i < shardsToCorrupt; i++ {
		n := rand.Intn(dataShards + parityShards)
		shards[n] = shards[n][:0]
	}
}

func benchmarkReconstruct(b *testing.B, dataShards, parityShards, shardSize int, opts ...Option) {
	o := []Option{WithAutoGoroutines(shardSize)}
	o = append(o, opts...)
	r, err := New(dataShards, parityShards, testOptions(o...)...)
	if err != nil {
		b.Fatal(err)
	}
	shards := r.(Extensions).AllocAligned(shardSize)

	for s := 0; s < dataShards; s++ {
		fillRandom(shards[s])
	}
	err = r.Encode(shards)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(shardSize * (dataShards + parityShards)))
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		corruptRandom(shards, dataShards, parityShards)

		err = r.Reconstruct(shards)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark 10 data slices with 2 parity slices holding 10000 bytes each
func BenchmarkReconstruct10x2x10000(b *testing.B) {
	benchmarkReconstruct(b, 10, 2, 10000)
}

// Benchmark 800 data slices with 200 parity slices
func BenchmarkReconstruct800x200(b *testing.B) {
	for size := 64; size <= 1<<20; size *= 4 {
		b.Run(fmt.Sprintf("%v", size), func(b *testing.B) {
			benchmarkReconstruct(b, 800, 200, size)
		})
	}
}

// Benchmark 50 data slices with 5 parity slices holding 100000 bytes each
func BenchmarkReconstruct50x5x50000(b *testing.B) {
	benchmarkReconstruct(b, 50, 5, 100000)
}

// Benchmark 10 data slices with 2 parity slices holding 1MB bytes each
func BenchmarkReconstruct10x2x1M(b *testing.B) {
	benchmarkReconstruct(b, 10, 2, 1024*1024)
}

// Benchmark 5 data slices with 2 parity slices holding 1MB bytes each
func BenchmarkReconstruct5x2x1M(b *testing.B) {
	benchmarkReconstruct(b, 5, 2, 1024*1024)
}

// Benchmark 10 data slices with 4 parity slices holding 1MB bytes each
func BenchmarkReconstruct10x4x1M(b *testing.B) {
	benchmarkReconstruct(b, 10, 4, 1024*1024)
}

// Benchmark 5 data slices with 2 parity slices holding 1MB bytes each
func BenchmarkReconstruct50x20x1M(b *testing.B) {
	benchmarkReconstruct(b, 50, 20, 1024*1024)
}

// Benchmark 5 data slices with 2 parity slices holding 1MB bytes each
func BenchmarkReconstructLeopard50x20x1M(b *testing.B) {
	benchmarkReconstruct(b, 50, 20, 1024*1024, WithLeopardGF(true), WithInversionCache(true))
}

// Benchmark 10 data slices with 4 parity slices holding 16MB bytes each
func BenchmarkReconstruct10x4x16M(b *testing.B) {
	benchmarkReconstruct(b, 10, 4, 16*1024*1024)
}

func corruptRandomData(shards [][]byte, dataShards, parityShards int) {
	shardsToCorrupt := rand.Intn(parityShards) + 1
	for i := 1; i <= shardsToCorrupt; i++ {
		n := rand.Intn(dataShards)
		shards[n] = shards[n][:0]
	}
}

func benchmarkReconstructData(b *testing.B, dataShards, parityShards, shardSize int) {
	r, err := New(dataShards, parityShards, testOptions(WithAutoGoroutines(shardSize))...)
	if err != nil {
		b.Fatal(err)
	}
	shards := r.(Extensions).AllocAligned(shardSize)

	for s := 0; s < dataShards; s++ {
		fillRandom(shards[s])
	}
	err = r.Encode(shards)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(shardSize * (dataShards + parityShards)))
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		corruptRandomData(shards, dataShards, parityShards)

		err = r.ReconstructData(shards)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark 10 data slices with 2 parity slices holding 10000 bytes each
func BenchmarkReconstructData10x2x10000(b *testing.B) {
	benchmarkReconstructData(b, 10, 2, 10000)
}

// Benchmark 800 data slices with 200 parity slices
func BenchmarkReconstructData800x200(b *testing.B) {
	for size := 64; size <= 1<<20; size *= 4 {
		b.Run(fmt.Sprintf("%v", size), func(b *testing.B) {
			benchmarkReconstructData(b, 800, 200, size)
		})
	}
}

// Benchmark 50 data slices with 5 parity slices holding 100000 bytes each
func BenchmarkReconstructData50x5x50000(b *testing.B) {
	benchmarkReconstructData(b, 50, 5, 100000)
}

// Benchmark 10 data slices with 2 parity slices holding 1MB bytes each
func BenchmarkReconstructData10x2x1M(b *testing.B) {
	benchmarkReconstructData(b, 10, 2, 1024*1024)
}

// Benchmark 5 data slices with 2 parity slices holding 1MB bytes each
func BenchmarkReconstructData5x2x1M(b *testing.B) {
	benchmarkReconstructData(b, 5, 2, 1024*1024)
}

// Benchmark 10 data slices with 4 parity slices holding 1MB bytes each
func BenchmarkReconstructData10x4x1M(b *testing.B) {
	benchmarkReconstructData(b, 10, 4, 1024*1024)
}

// Benchmark 5 data slices with 2 parity slices holding 1MB bytes each
func BenchmarkReconstructData50x20x1M(b *testing.B) {
	benchmarkReconstructData(b, 50, 20, 1024*1024)
}

// Benchmark 10 data slices with 4 parity slices holding 16MB bytes each
func BenchmarkReconstructData10x4x16M(b *testing.B) {
	benchmarkReconstructData(b, 10, 4, 16*1024*1024)
}

func benchmarkReconstructP(b *testing.B, dataShards, parityShards, shardSize int) {
	r, err := New(dataShards, parityShards, testOptions(WithMaxGoroutines(1))...)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(shardSize * (dataShards + parityShards)))
	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		shards := r.(Extensions).AllocAligned(shardSize)

		for s := 0; s < dataShards; s++ {
			fillRandom(shards[s])
		}
		err = r.Encode(shards)
		if err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		for pb.Next() {
			corruptRandom(shards, dataShards, parityShards)

			err = r.Reconstruct(shards)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// Benchmark 10 data slices with 2 parity slices holding 10000 bytes each
func BenchmarkReconstructP10x2x10000(b *testing.B) {
	benchmarkReconstructP(b, 10, 2, 10000)
}

// Benchmark 10 data slices with 5 parity slices holding 20000 bytes each
func BenchmarkReconstructP10x5x20000(b *testing.B) {
	benchmarkReconstructP(b, 10, 5, 20000)
}

func TestEncoderReconstruct(t *testing.T) {
	parallelIfNotShort(t)
	testEncoderReconstruct(t)
	for _, o := range testOpts() {
		testEncoderReconstruct(t, o...)
	}
}

func testEncoderReconstruct(t *testing.T, o ...Option) {
	// Create some sample data
	var data = make([]byte, 250<<10)
	fillRandom(data)

	// Create 5 data slices of 50000 elements each
	enc, err := New(7, 6, testOptions(o...)...)
	if err != nil {
		t.Fatal(err)
	}
	shards, err := enc.Split(data)
	if err != nil {
		t.Fatal(err)
	}
	err = enc.Encode(shards)
	if err != nil {
		t.Fatal(err)
	}

	// Check that it verifies
	ok, err := enc.Verify(shards)
	if !ok || err != nil {
		t.Fatal("not ok:", ok, "err:", err)
	}

	// Delete a shard
	shards[0] = nil

	// Should reconstruct
	err = enc.Reconstruct(shards)
	if err != nil {
		t.Fatal(err)
	}

	// Check that it verifies
	ok, err = enc.Verify(shards)
	if !ok || err != nil {
		t.Fatal("not ok:", ok, "err:", err)
	}

	// Recover original bytes
	buf := new(bytes.Buffer)
	err = enc.Join(buf, shards, len(data))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), data) {
		t.Fatal("recovered bytes do not match")
	}

	// Corrupt a shard
	shards[0] = nil
	shards[1][0], shards[1][500] = 75, 75

	// Should reconstruct (but with corrupted data)
	err = enc.Reconstruct(shards)
	if err != nil {
		t.Fatal(err)
	}

	// Check that it verifies
	ok, err = enc.Verify(shards)
	if ok || err != nil {
		t.Fatal("error or ok:", ok, "err:", err)
	}

	// Recovered data should not match original
	buf.Reset()
	err = enc.Join(buf, shards, len(data))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(buf.Bytes(), data) {
		t.Fatal("corrupted data matches original")
	}
}

func TestSplitJoin(t *testing.T) {
	opts := [][]Option{
		testOptions(),
		append(testOptions(), WithLeopardGF(true)),
		append(testOptions(), WithLeopardGF16(true)),
	}
	for i, opts := range opts {
		t.Run("opt-"+strconv.Itoa(i), func(t *testing.T) {
			for _, dp := range [][2]int{{1, 0}, {5, 0}, {5, 1}, {12, 4}, {2, 15}, {17, 1}} {
				enc, _ := New(dp[0], dp[1], opts...)
				ext := enc.(Extensions)

				_, err := enc.Split([]byte{})
				if err != ErrShortData {
					t.Errorf("expected %v, got %v", ErrShortData, err)
				}

				buf := new(bytes.Buffer)
				err = enc.Join(buf, [][]byte{}, 0)
				if err != ErrTooFewShards {
					t.Errorf("expected %v, got %v", ErrTooFewShards, err)
				}
				for _, size := range []int{ext.DataShards(), 1337, 2699} {
					for _, extra := range []int{0, 1, ext.ShardSizeMultiple(), ext.ShardSizeMultiple() * ext.DataShards(), ext.ShardSizeMultiple()*ext.ParityShards() + 1, 255} {
						buf.Reset()
						t.Run(fmt.Sprintf("d-%d-p-%d-sz-%d-cap%d", ext.DataShards(), ext.ParityShards(), size, extra), func(t *testing.T) {
							var data = make([]byte, size, size+extra)
							var ref = make([]byte, size, size)
							fillRandom(data)
							copy(ref, data)

							shards, err := enc.Split(data)
							if err != nil {
								t.Fatal(err)
							}
							err = enc.Encode(shards)
							if err != nil {
								t.Fatal(err)
							}
							_, err = enc.Verify(shards)
							if err != nil {
								t.Fatal(err)
							}
							for i := range shards[:ext.ParityShards()] {
								// delete data shards up to parity
								shards[i] = nil
							}
							err = enc.Reconstruct(shards)
							if err != nil {
								t.Fatal(err)
							}

							// Rejoin....
							err = enc.Join(buf, shards, size)
							if err != nil {
								t.Fatal(err)
							}
							if !bytes.Equal(buf.Bytes(), ref) {
								t.Log("")
								t.Fatal("recovered data does match original")
							}

							err = enc.Join(buf, shards, len(data)+ext.DataShards()*ext.ShardSizeMultiple())
							if err != ErrShortData {
								t.Errorf("expected %v, got %v", ErrShortData, err)
							}

							shards[0] = nil
							err = enc.Join(buf, shards, len(data))
							if err != ErrReconstructRequired {
								t.Errorf("expected %v, got %v", ErrReconstructRequired, err)
							}
						})
					}
				}
			}
		})
	}
}

func TestCodeSomeShards(t *testing.T) {
	var data = make([]byte, 250000)
	fillRandom(data)
	enc, _ := New(5, 3, testOptions()...)
	r := enc.(*reedSolomon) // need to access private methods
	shards, _ := enc.Split(data)

	old := runtime.GOMAXPROCS(1)
	r.codeSomeShards(r.parity, shards[:r.dataShards], shards[r.dataShards:r.dataShards+r.parityShards], len(shards[0]))

	// hopefully more than 1 CPU
	runtime.GOMAXPROCS(runtime.NumCPU())
	r.codeSomeShards(r.parity, shards[:r.dataShards], shards[r.dataShards:r.dataShards+r.parityShards], len(shards[0]))

	// reset MAXPROCS, otherwise testing complains
	runtime.GOMAXPROCS(old)
}

func TestStandardMatrices(t *testing.T) {
	if testing.Short() || runtime.GOMAXPROCS(0) < 4 {
		// Runtime ~15s.
		t.Skip("Skipping slow matrix check")
	}
	for i := 1; i < 256; i++ {
		i := i
		t.Run(fmt.Sprintf("x%d", i), func(t *testing.T) {
			parallelIfNotShort(t)
			// i == n.o. datashards
			var shards = make([][]byte, 255)
			for p := range shards {
				v := byte(i)
				shards[p] = []byte{v}
			}
			rng := rand.New(rand.NewSource(0))
			for j := 1; j < 256; j++ {
				// j == n.o. parity shards
				if i+j > 255 {
					continue
				}
				sh := shards[:i+j]
				r, err := New(i, j, testOptions(WithFastOneParityMatrix())...)
				if err != nil {
					// We are not supposed to write to t from goroutines.
					t.Fatal("creating matrix size", i, j, ":", err)
				}
				err = r.Encode(sh)
				if err != nil {
					t.Fatal("encoding", i, j, ":", err)
				}
				for k := 0; k < j; k++ {
					// Remove random shard.
					r := int(rng.Int63n(int64(i + j)))
					sh[r] = sh[r][:0]
				}
				err = r.Reconstruct(sh)
				if err != nil {
					t.Fatal("reconstructing", i, j, ":", err)
				}
				ok, err := r.Verify(sh)
				if err != nil {
					t.Fatal("verifying", i, j, ":", err)
				}
				if !ok {
					t.Fatal(i, j, ok)
				}
				for k := range sh {
					if k == i {
						// Only check data shards
						break
					}
					if sh[k][0] != byte(i) {
						t.Fatal("does not match", i, j, k, sh[0], sh[k])
					}
				}
			}
		})
	}
}

func TestCauchyMatrices(t *testing.T) {
	if testing.Short() || runtime.GOMAXPROCS(0) < 4 {
		// Runtime ~15s.
		t.Skip("Skipping slow matrix check")
	}
	for i := 1; i < 256; i++ {
		i := i
		t.Run(fmt.Sprintf("x%d", i), func(t *testing.T) {
			parallelIfNotShort(t)
			var shards = make([][]byte, 255)
			for p := range shards {
				v := byte(i)
				shards[p] = []byte{v}
			}
			rng := rand.New(rand.NewSource(0))
			for j := 1; j < 256; j++ {
				// j == n.o. parity shards
				if i+j > 255 {
					continue
				}
				sh := shards[:i+j]
				r, err := New(i, j, testOptions(WithCauchyMatrix(), WithFastOneParityMatrix())...)
				if err != nil {
					// We are not supposed to write to t from goroutines.
					t.Fatal("creating matrix size", i, j, ":", err)
				}
				err = r.Encode(sh)
				if err != nil {
					t.Fatal("encoding", i, j, ":", err)
				}
				for k := 0; k < j; k++ {
					// Remove random shard.
					r := int(rng.Int63n(int64(i + j)))
					sh[r] = sh[r][:0]
				}
				err = r.Reconstruct(sh)
				if err != nil {
					t.Fatal("reconstructing", i, j, ":", err)
				}
				ok, err := r.Verify(sh)
				if err != nil {
					t.Fatal("verifying", i, j, ":", err)
				}
				if !ok {
					t.Fatal(i, j, ok)
				}
				for k := range sh {
					if k == i {
						// Only check data shards
						break
					}
					if sh[k][0] != byte(i) {
						t.Fatal("does not match", i, j, k, sh[0], sh[k])
					}
				}
			}
		})
	}
}

func TestPar1Matrices(t *testing.T) {
	if testing.Short() || runtime.GOMAXPROCS(0) < 4 {
		// Runtime ~15s.
		t.Skip("Skipping slow matrix check")
	}
	for i := 1; i < 256; i++ {
		i := i
		t.Run(fmt.Sprintf("x%d", i), func(t *testing.T) {
			parallelIfNotShort(t)
			var shards = make([][]byte, 255)
			for p := range shards {
				v := byte(i)
				shards[p] = []byte{v}
			}
			rng := rand.New(rand.NewSource(0))
			for j := 1; j < 256; j++ {
				// j == n.o. parity shards
				if i+j > 255 {
					continue
				}
				sh := shards[:i+j]
				r, err := New(i, j, testOptions(WithPAR1Matrix())...)
				if err != nil {
					// We are not supposed to write to t from goroutines.
					t.Fatal("creating matrix size", i, j, ":", err)
				}
				err = r.Encode(sh)
				if err != nil {
					t.Fatal("encoding", i, j, ":", err)
				}
				for k := 0; k < j; k++ {
					// Remove random shard.
					r := int(rng.Int63n(int64(i + j)))
					sh[r] = sh[r][:0]
				}
				err = r.Reconstruct(sh)
				if err != nil {
					if err == errSingular {
						t.Logf("Singular: %d (data), %d (parity)", i, j)
						for p := range sh {
							if len(sh[p]) == 0 {
								shards[p] = []byte{byte(i)}
							}
						}
						continue
					}
					t.Fatal("reconstructing", i, j, ":", err)
				}
				ok, err := r.Verify(sh)
				if err != nil {
					t.Fatal("verifying", i, j, ":", err)
				}
				if !ok {
					t.Fatal(i, j, ok)
				}
				for k := range sh {
					if k == i {
						// Only check data shards
						break
					}
					if sh[k][0] != byte(i) {
						t.Fatal("does not match", i, j, k, sh[0], sh[k])
					}
				}
			}
		})
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		data, parity int
		err          error
	}{
		{127, 127, nil},
		{128, 128, nil},
		{255, 1, nil},
		{255, 0, nil},
		{1, 0, nil},
		{65536, 65536, ErrMaxShardNum},

		{0, 1, ErrInvShardNum},
		{1, -1, ErrInvShardNum},
		{65636, 1, ErrMaxShardNum},

		// overflow causes r.Shards to be negative
		{256, int(^uint(0) >> 1), errInvalidRowSize},
	}
	for _, test := range tests {
		_, err := New(test.data, test.parity, testOptions()...)
		if err != test.err {
			t.Errorf("New(%v, %v): expected %v, got %v", test.data, test.parity, test.err, err)
		}
	}
}

func TestSplitZero(t *testing.T) {
	data := make([]byte, 512)
	for _, opts := range testOpts() {
		ecctest, err := New(1, 0, opts...)
		if err != nil {
			t.Fatal(err)
		}
		_, err = ecctest.Split(data)
		if err != nil {
			t.Fatal(err)
		}
	}
}

// Benchmark 10 data shards and 4 parity shards and 160MB data.
func BenchmarkSplit10x4x160M(b *testing.B) {
	benchmarkSplit(b, 10, 4, 160*1024*1024)
}

// Benchmark 5 data shards and 2 parity shards with 5MB data.
func BenchmarkSplit5x2x5M(b *testing.B) {
	benchmarkSplit(b, 5, 2, 5*1024*1024)
}

// Benchmark 1 data shards and 2 parity shards with 1MB data.
func BenchmarkSplit10x2x1M(b *testing.B) {
	benchmarkSplit(b, 10, 2, 1024*1024)
}

// Benchmark 10 data shards and 4 parity shards with 10MB data.
func BenchmarkSplit10x4x10M(b *testing.B) {
	benchmarkSplit(b, 10, 4, 10*1024*1024)
}

// Benchmark 50 data shards and 20 parity shards with 50MB data.
func BenchmarkSplit50x20x50M(b *testing.B) {
	benchmarkSplit(b, 50, 20, 50*1024*1024)
}

// Benchmark 17 data shards and 3 parity shards with 272MB data.
func BenchmarkSplit17x3x272M(b *testing.B) {
	benchmarkSplit(b, 17, 3, 272*1024*1024)
}

func benchmarkSplit(b *testing.B, shards, parity, dataSize int) {
	r, err := New(shards, parity, testOptions(WithAutoGoroutines(dataSize))...)
	if err != nil {
		b.Fatal(err)
	}

	data := make([]byte, dataSize)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = r.Split(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkParallel(b *testing.B, dataShards, parityShards, shardSize int) {
	// Run max 1 goroutine per operation.
	r, err := New(dataShards, parityShards, testOptions(WithMaxGoroutines(1))...)
	if err != nil {
		b.Fatal(err)
	}
	c := runtime.GOMAXPROCS(0)

	// Note that concurrency also affects total data size and will make caches less effective.
	if testing.Verbose() {
		b.Log("Total data:", (c*dataShards*shardSize)>>20, "MiB", "parity:", (c*parityShards*shardSize)>>20, "MiB")
	}
	// Create independent shards
	shardsCh := make(chan [][]byte, c)
	for i := 0; i < c; i++ {
		shards := r.(Extensions).AllocAligned(shardSize)

		for s := 0; s < dataShards; s++ {
			fillRandom(shards[s])
		}
		shardsCh <- shards
	}

	b.SetBytes(int64(shardSize * (dataShards + parityShards)))
	b.SetParallelism(c)
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			shards := <-shardsCh
			err = r.Encode(shards)
			if err != nil {
				b.Fatal(err)
			}
			shardsCh <- shards
		}
	})
}

func BenchmarkParallel_8x8x64K(b *testing.B)   { benchmarkParallel(b, 8, 8, 64<<10) }
func BenchmarkParallel_8x8x05M(b *testing.B)   { benchmarkParallel(b, 8, 8, 512<<10) }
func BenchmarkParallel_20x10x05M(b *testing.B) { benchmarkParallel(b, 20, 10, 512<<10) }
func BenchmarkParallel_8x8x1M(b *testing.B)    { benchmarkParallel(b, 8, 8, 1<<20) }
func BenchmarkParallel_8x8x8M(b *testing.B)    { benchmarkParallel(b, 8, 8, 8<<20) }
func BenchmarkParallel_8x8x32M(b *testing.B)   { benchmarkParallel(b, 8, 8, 32<<20) }

func BenchmarkParallel_8x3x1M(b *testing.B) { benchmarkParallel(b, 8, 3, 1<<20) }
func BenchmarkParallel_8x4x1M(b *testing.B) { benchmarkParallel(b, 8, 4, 1<<20) }
func BenchmarkParallel_8x5x1M(b *testing.B) { benchmarkParallel(b, 8, 5, 1<<20) }

func TestReentrant(t *testing.T) {
	for optN, o := range testOpts() {
		for _, size := range testSizes() {
			data, parity := size[0], size[1]
			rng := rand.New(rand.NewSource(0xabadc0cac01a))
			t.Run(fmt.Sprintf("opt-%d-%dx%d", optN, data, parity), func(t *testing.T) {
				perShard := 16384 + 1
				if testing.Short() {
					perShard = 1024 + 1
				}
				r, err := New(data, parity, testOptions(o...)...)
				if err != nil {
					t.Fatal(err)
				}
				x := r.(Extensions)
				if want, got := data, x.DataShards(); want != got {
					t.Errorf("DataShards returned %d, want %d", got, want)
				}
				if want, got := parity, x.ParityShards(); want != got {
					t.Errorf("ParityShards returned %d, want %d", got, want)
				}
				if want, got := parity+data, x.TotalShards(); want != got {
					t.Errorf("TotalShards returned %d, want %d", got, want)
				}
				mul := x.ShardSizeMultiple()
				if mul <= 0 {
					t.Fatalf("Got unexpected ShardSizeMultiple: %d", mul)
				}
				perShard = ((perShard + mul - 1) / mul) * mul
				runs := 10
				if testing.Short() {
					runs = 2
				}
				for i := 0; i < runs; i++ {
					shards := AllocAligned(data+parity, perShard)

					err = r.Encode(shards)
					if err != nil {
						t.Fatal(err)
					}
					ok, err := r.Verify(shards)
					if err != nil {
						t.Fatal(err)
					}
					if !ok {
						t.Fatal("Verification failed")
					}

					if parity == 0 {
						// Check that Reconstruct and ReconstructData do nothing
						err = r.ReconstructData(shards)
						if err != nil {
							t.Fatal(err)
						}
						err = r.Reconstruct(shards)
						if err != nil {
							t.Fatal(err)
						}

						// Skip integrity checks
						continue
					}

					// Delete one in data
					idx := rng.Intn(data)
					want := shards[idx]
					shards[idx] = nil

					err = r.ReconstructData(shards)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(shards[idx], want) {
						t.Fatal("did not ReconstructData correctly")
					}

					// Delete one randomly
					idx = rng.Intn(data + parity)
					want = shards[idx]
					shards[idx] = nil
					err = r.Reconstruct(shards)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(shards[idx], want) {
						t.Fatal("did not Reconstruct correctly")
					}

					err = r.Encode(make([][]byte, 1))
					if err != ErrTooFewShards {
						t.Errorf("expected %v, got %v", ErrTooFewShards, err)
					}

					// Make one too short.
					shards[idx] = shards[idx][:perShard-1]
					err = r.Encode(shards)
					if err != ErrShardSize {
						t.Errorf("expected %v, got %v", ErrShardSize, err)
					}
				}
			})
		}
	}
}

func TestRsCodec(t *testing.T) {

	//msg := []byte("例句\n（汉）司马迁《史记》 ：用太白守之天下学校散文儒失业兵戈大兴荧惑守。（所引不见于《史记》，亦不详出处，依文义，似当读作：用太白守之，天下学校散，文儒失业，兵戈大兴，荧惑守[后当有缺字，文义不足]。）\n（汉）袁康《越绝书》越绝卷第十三 ：其终始即尊位倾万物散文武之业桀纣之迹可知矣。（引文见《越绝书》卷十三《枕中》：范子曰：“臣闻古之贤主圣君，执中和而原其终始，即位安而万物定矣；不执中和，不原其终始，即尊位倾，万物散。文武之业，桀纣")
	msg := []byte("例句\n（汉）司马迁《史记》 ：用太白守之天下学校散文儒失业兵戈大兴荧惑守。（所引不见于《史记》，亦不详出处，依文义，似当读作：用太白守之，天下学校散，文儒失业，兵戈大兴，荧惑守[后当有缺字，文义不足]。）\n（汉）袁康《越绝书》越绝卷第十三 ：其终始即尊位倾万物散文武之业桀纣之迹可知矣。（引文见《越绝书》卷十三《枕中》：范子曰：“臣闻古之贤主圣君，执中和而原其终始，即位安而万物定矣；不执中和，不原其终始，即尊位倾，万物散。文武之业，桀纣之迹，可知矣。”这里“文武”指周文王和周武王，与其后之“桀纣”相对，前“散”字与“文”字义无涉。）\n引证解释\n1.文采焕发。晋木华《海赋》：“若乃云锦散文於沙汭之际，绫罗被光於螺蚌之节。”木华，字玄虚，广川人。《海赋》见《昭明文选》卷十二。所此句下注曰：“言沙汭之际，文若云锦；螺蚌之节，光若绫罗。毛苌《诗传》曰：“芮，涯也。”芮与汭通。曹植《齐瑟行》：‘蚌蛤被滨涯，光采如锦红。’”\n2.犹行文。 南朝梁刘勰《文心雕龙·明诗》：“观其结体散文，直而不野，婉转附物，怊怅切情：实五言之冠冕也。”（此所引为见《文心雕龙》卷二《明诗》第六，为评论《古诗十九首》之句子。此与上句《海赋》中引文，俱解释为散布、铺陈的意思。）\n3.文体名。凡是不押韵、不重排偶的散体文章，概称散文。随着文学概念的演变和文学体裁的发展，散文的概念也时有变化，在某些历史时期又将小说与其他抒情、记事的文学作品统称为散文，以区别于讲求韵律的诗歌。现代散文是指除小说、诗歌、戏剧等文学体裁之外的其他文学作品。其本身按其内容和形式的不同，又可分为杂文、小品、随笔等。\n概念\n散文是指以文字为创作、审美对象的文学艺术体裁，是文学中的一种体裁形式。\n1.在中国古代文学中，散文与韵文、骈文相对，不追求押韵和句式的工整。这是广义上的散文。\n2.在中国现代文学中，散文指与诗歌、小说、戏剧并行的一种文学体裁。这是狭义上的散文。\n特点\n形散神聚：“形散”既指题材广泛、写法多样，又指结构自由、不拘一格；“神聚”既指中心集中，又指有贯穿全文的线索。散文写人写事从根本上写的是情感体验。情感体验就是“不散的神”，而人与事则是“散”的可有可无、可多可少的“形”。\n“形散”主要是说散文取材十分广泛自由，不受时间和空间的限制，表现手法不拘一格。可以叙述事件的发展，可以描写人物形象，可以托物抒情，可以发表议论，而且作者可以根据内容需要自由调整、随意变化。“神不散”主要是从散文的立意方面说的，即散文所要表达的主题必须明确而集中，无论散文的内容多么广泛，表现手法多么灵活，无不为更好的表达主题服务。\n意境深邃：注重表现作者的生活感受，抒情性强，情感真挚。\n作者借助想象与联想，由此及彼，由浅入深，由实而虚的依次写来，可以融情于景、寄情于事、寓情于物、托物言志，表达作者的真情实感，实现物我的统一，展现出更深远的思想，使读者领会更深的道理。\n语言优美：所谓优美，就是指散文的语言清新明丽（也美丽），生动活泼，富于音乐感，行文如涓涓流水，叮咚有声，如娓娓而谈，情真意切。所谓凝练，是说散文的语言简洁质朴，自然流畅，寥寥数语就可以描绘出生动的形象，勾勒出动人的场景，显示出深远的意境。散文力求写景如在眼前，写情沁人心脾。\n散文素有“美文”之称，它除了有精神的见解、优美的意境外，还有清新隽永、质朴无华的文采。经常读一些好的散文，不仅可以丰富知识、开阔眼界，培养高尚的思想情操，还可以从中学习选材立意、谋篇布局和遣词造句的技巧，提高自己的语言表达能力。\n《列子·黄帝》一篇，见有列子“乘风而归”的说法。又有列子对尹生说的一段话：“心凝形释，骨肉都融，不觉形之所倚，足之所履，随风东西，犹木叶干壳。意不知风乘我耶？我乘风乎？”这里的“心”与“神”相通，张湛注《列子》即把“心凝形释”说成“神凝形废”了。\n什么叫做“神凝”呢？《黄帝》篇里就有“用志不分，乃疑（通凝）于神”的话。指用心专一。当然，这“神”与“凝”，都不是停滞的、枯死的，而是如《周易·系辞·上》所说：“唯神也，故不疾而速，不行而至。”也就是说，“神”是可以超越空间而自由驰骋的。具体到文章写作，也就是如上文所说，“神”是有趋向性的，富于动感的。\n至于“形”的含义，《乐记》里有“在天成象，在地成形”的话。钱锺书先生释为“‘形’者，完成之定状”。钱先生还引述亚里士多德论“自然”有五层含义。其四，是“相形之下，尚未成形之原料”，也就是“有质而无形”的状态；其五，是“止境宿归之形”。这种由“原质”，“原料”而“成形”的说法用之于文章写作，也如钱先生所阐述的，“春来花鸟，具‘形’之天然物色也，而性癖耽吟者反目为‘诗料’”。指明做为“诗料”的“形”，即包括着“题材”的内。“吟安佳句，具‘形’之词章也”。指明做为诗文的“形”即指“词章”，包括语言、结构等。我在上文所论“形”的概念，也具有同这里所引说法的一致性。\n总起来看，论述散文创作的某种特色所惯常运用的提法“形散神不散”，其“神”与“形”的含义许是取喻于《列子》“神凝形释”的。而运用“神凝形散”或“神收形放”一类话来赞美散文的构思谋篇，在概念上虽属借喻，但是同《列子》的提法具有相当的对应的类比性质，且用语简括，概念现成，有较强的表现力。那么，散文研究领域里的“形神”说之所以被承认，被沿用，原因之一，正在于此。\n线索\n线索是作者将材料串联起来的“红线”或“寄托物”。常见的线索有以下几类：\n1、以核心人物为线索。\n2、以核心事物为线索。\n3、以时间为线索。\n4、以地点为线索。\n5、以作者的情感变化为线索。\n6、以主要事件的发展为线索。\n需要注意的是，线索的类型及其在具体文章中的表现形式是多种多样的。有的文章线索单一；有的文章线索双重，或虚实结合，或纵横交叉，或一主一次，或平行发展。线索在文中的体现，多半在标题、开头、结尾和过渡段的段首、段尾等处；而把握文章的气势、整体脉络和倾向，则是把握线索的关键。\n两种解释\n古代文学中：散文包括古文、骈文和辞赋，骈文和辞赋基本上属于韵文范畴，但在行文体制上更接近散文。\n现代文学中：指诗歌、小说、戏剧以外的文学作品和文学体裁，包括杂文、随笔、游记等，对它又有广义和狭义两种理解。\n广义的散文，是指诗歌、小说、戏剧以外的所有具有文学性的散行文章。除以议论抒情为主的散文外，还包括通讯、报告文学、随笔杂文、回忆录、传记等文体。随着写作学科的发展，许多文体自立门户，散文的范围日益缩小。\n狭义的散文是指文艺性散文，它是一种以记叙或抒情为主，取材广泛、笔法灵活、篇幅短小、情文并茂的文学样式。\n常见的散文有叙事散文、抒情散文和议论散文。\n分类\n播报\n编辑\n叙事散文\n叙事散文，或称记叙散文，以叙事为主，叙事情节不求完整，但很集中，叙事中的情渗透在字里行间。侧重于从叙述人物和事件的发展变化过程中反映事物的本质，具有时间、地点、人物、事件等因素，从一个角度选取题材，表现作者的思想感情。根据该类散文内容的侧重点不同，又可将它区分为记事散文和写人散文。\n偏重于记事\n以事件发展为线索，偏重对事件的叙述。它可以是一个有头有尾的故事，如许地山的《落花生》，也可以是几个片断的剪辑，如鲁迅的《从百草园到三味书屋》。在叙事中倾注作者真挚的感情，这是与小说叙事最显著的区别。\n偏重于记人\n全篇以人物为中心。它往往抓住人物的性格特征作粗线条勾勒，偏重表现人物的基本气质、性格和精神面貌，如鲁迅《藤野先生》，人物形象是否真实是它与小说的区别。\n抒情散文\n抒情散文，或称写景散文，指以描绘景物、抒发作者对现实生活的感受、激情和意愿的散文。\n注重表现作者的思想感受，抒发作者的思想感情。这类散文有对具体事物的记叙和描绘，但通常没有贯穿全篇的情节，其突出的特点是强烈的抒情性。它或直抒胸臆，或触景生情，洋溢着浓烈的诗情画意，即使描写的是自然风物，也赋予了深刻的社会内容和思想感情。优秀的抒情散文感情真挚，语言生动，还常常运用象征和比拟的手法，把思想寓于形象之中，具有强烈的艺术感染力。例如：茅盾的《白杨礼赞》、魏巍的《依依惜别的深情》、朱自清的《荷塘月色》、冰心的《樱花赞》。\n以描绘景物为主的。这类文章多是在描绘景物的同时抒发感情，或借景抒情，或寓情于景，抓住景物的特征，按照空间的变换顺序，运用移步换景的方法，把观察的变化作为全文的脉络。生动的景物描绘，不但可以交代背景，渲染气氛，而且可以烘托人物的思想感情，更好的表现主题。例如：刘白羽的《长江三峡》。\n哲理散文\n哲理，是感悟的参透，思想的火花，理念的凝聚，睿智的结晶。它纵贯古今，横亘中外，包容大千世界，穿透人生社会，寄寓于人生百态家长里短，闪思维领域万千景观。 高明的作者，善于抓住哲理闪光的瞬间，形诸笔墨，写就内涵丰厚、耐人寻味的美文。时常涵咏这类美文，自然能在潜移默化中受到启迪和熏陶，洗礼和升华，这种内化作用无疑是巨大的。\n哲理散文以种种形象来参与生命的真理，从而揭露万物之间的永恒相似，它因其深邃性和心灵透辟的整合，给我们一种透过现象深入本质、揭示事物的底蕴、观念具有震撼性的审美效果。把握哲理散文体现出的思维方式，去体悟哲理散文所蕴藏的深厚的文化底蕴和文化积淀。例如：尼采的《我的灵魂》。\n1.哲理散文中的象征思维：哲理散文因为超越日常经验的意义和自身的自然物理性质，构成了本体的象征表达。它摒弃的是浅薄，而是达到一种与人的思想情性相通、生命交感、灵气往来的境界，我们从象征中获得理性的醒悟和精神的畅快，由心灵的平静转到灵魂的震颤，超越一般情感反应而居于精神的顶端。\n2.哲理散文的联想思维：由于哲理散文是个立体的、综合的思维体系，经过联想，文章拥有更丰富的内涵，不至于显得单薄，把自然、社会、人生多个角度进行了融合。\n3.哲理散文中的情感思维：哲理散文在本质意义上是思想表达对情感的一种依赖。“外师造化，中得心源”，由于作者对生活的感悟过程中有情感参与，理解的结果有情感及想象的融入，所以哲理散文中的思想，就不是一般干巴巴的议论，而是寓含了生活情感的思想，是蘸满了审美情感液汁的思想。从哲理散文的字里行间去读解到心智的深邃，理解生命的本义。这就是哲理散文艺术美之所在。\n鉴赏技法\n播报\n编辑\n散文鉴赏，重点是把握其“形”与“神”的关系。散文鉴赏应注意以下几点：\n1.读散文要识得“文眼”\n凡是构思精巧、富有意境或写得含蓄的诗文，往往都有“眼”的安置。鉴赏散文时，要全力找出能揭示全篇旨趣和有画龙点睛妙用的“文眼”，以便领会作者为文的缘由与目的。“文眼”的设置因文而异，可以是一个字、一句话、一个细节、一缕情丝，乃至一景一物。并非每篇散文都有必要的“文眼”。\n2.注意散文表现手法的特点\n注意散文表现手法的特点，深入体会文章的内容。\n散文常常托物寄意，为了使读者具体感受到所寄寓的丰富内涵，作者常常对所写的事物作细致的描绘和精心的刻画，就是所谓的“形得而神自来焉”。我们读文章就要抓住“形”的特点，由“形”见“神”，深入体会文章内容。\n3.注意展开联想，领会文章的神韵\n联想的方式有：①串联式：如《猎户》“尚二叔→百中老人→董昆”；②辐射式：如《土地》以“土地”为中心生发开去，写“热爱生活，保卫土地，建设土地”；③假托式：如《白杨礼赞》；④屏风式：如《风景谈》。注意丰富的联想，由此及彼，由浅入深，由实到虚，这样才能体会到文章的神韵，领会到更深刻的道理。\n4.品味散文的语言\n一大特色是语言美。好散文语言凝练、优美，又自由灵活，接近口语。优美的散文，更是富于哲理、诗情、画意。杰出的散文家的语言又各具不同的语言风格：鲁迅的散文语言精练深邃，茅盾的散文语言细腻深刻，郭沫若的散文语言气势磅礴，巴金的散文语言朴素优美，朱自清的散文语言清新隽永，冰心的散文语言委婉明丽，孙犁的散文语言质朴，刘白羽的散文语言奔放，杨朔的散文语言精巧，何为的散文语言雅致。一些散文大家的语言，又常常因内容而异。如鲁迅的《纪念刘和珍君》的语言，锋利如匕首；《好的故事》的语言，绚丽如云锦；《风筝》的语言，凝重如深潭。体味散文的语言风格，就可以对散文的内容体味得更加深刻。\n5.领会作品的内涵\n阅读散文就要进行由此及彼举一反三的想像、联想和补充。把自己的想像和作者的想像融合在一起，丰富作品的意境和形象，填补文中的结构空间。\n鉴赏问题\n播报\n编辑\n1、整体入手，理清文章脉络。材料丰富，思路灵活是散文的主要特点之一，阅读时一定要着眼于文章的整体，注意理清内部的相互关系，从宏观上驾驭文章，体察作者寄寓其中的意，倾注其中的情。如《长城》（2000年）一文，从深秋晚景写起，引入对历史的回顾与反思，再从历史回到现实，在历史与现实的对比中深化主旨，卒章显志，含蓄而又深沉。在这种整体阅读的基础上，再来回答题目，就会洞若观火，游刃有余。\n2、了解背景，透视创作历程。作品是社会的折射，内容是背景的产物。有不少散文的创作，往往受环境的影响。因此，了解文章的相关背景，是阅读鉴赏散文的一把钥匙。阅读《兽·人·鬼》（2000年春季），就必须认真阅读注释，分析背景材料。抗战胜利后，国统区人民掀起了反内战运动，国民党当局却大行不义，倒行逆施，制造了臭名昭著的“一二·一”惨案。闻一多先生十分悲愤，坚决主张声援学生的爱国运动，对个别教授畏首畏尾，保全小我的做法极为不满，于是写了这篇文章。透视创作历程，了解作者的创作意图和思想感情，再对照原文，试卷中的问题就不难找到答案。\n3、借助想象，体察作者情感。散文属于文学范畴，阅读散文必须发挥联想和想象，结合个人生活体验，和作者情感发生强烈共鸣。读《长城》，如果能联想到余秋雨在《都江堰》一文中对“长城”的议论，能想象到长城上狼烟四起，民族斗争的惨烈，想象到中华民族融合过程中的曲折历程，就不难触摸到作者那颗希望中华民族走出封闭与落后，走向繁荣与强大的赤诚滚烫的心。\n4、辨识手法，找准突破口。托物言志是散文常用的主要表现手法之一，托物言志类散文也多次高考试题中。如《报秋》（1999年），这是一篇章法严谨而又情文并茂的散文，深含着生活的哲理。作者通过玉簪花这个载体，提醒人们要多珍惜光阴，有所作为，不能虚度年华。这就是“玉簪花精神”。抓住这个“精神”，也就等于找准了阅读的突破口。\n5、明确技巧，提高答题效率。阅读散文，掌握一些常见的修辞手法和表达技巧，可以提高阅读效率，提高答题的正确率。常见的有：①比喻。如“兽”“鬼”各指什么（《人·兽·鬼》）；②反衬。如《报秋》中用太阳花反衬玉簪花生命力之强；③对比。如《青菜》（1993年）中，“高高翘起的狗尾巴草”，“自我炫耀的灯笼草”，“凌空悬挂的黄瓜”，与“紧紧依靠大地，朴素沉着的青菜”形成了鲜明的对比；④象征。如《门》（2001年）中的“门”；⑤排比。如“领取秋，领取冬，领取四季，领取生活”（《报秋》），层层铺开，逐步扩大，对点明主旨起到了强化作用；⑥变换人称。用“我”增强文章的真实性，用“你”便于抒情，便于对话，拉近与读者的距离，用“它”或“她”只是写了不同人的感受。\n6、瞻前顾后，分析句段关系。阅读散文时还要瞻前顾后，注意句与句之间，段与段之间的前后勾连。如《话说知音》（2002年），为什么说“知音的传说已经成为中国传统文化的一部分”呢？要回答这个问题，就必须理清前四段之间的关系。第一段写自从有了关于知音的传说后，人们对知音的神往和渴求；第二、三、四段写了关于知音的传说在历代典籍中的记载。综合这两部分，就回答了以上问题。二者缺其一，都不是完整的回答。\n最后需要指出的是，阅读散文还需注意文体特点。叙事散文讲求以小见大，形与神的关系是重点；写景散文注意情景交融，情与景的契合是关键；咏物散文托物言志，尽可能体味象征手法。但有一点更重要，那就是，阅读鉴赏散文要用自己的“心”去发现“散文的心”，用自己的人生体验和智慧去解读“作者心灵弹奏的歌声”。\n发展历程\n播报\n编辑\n先秦\n包括诸子散文和历史散文。诸子散文以论说为主，如《论语》、《孟子》、《庄子》；历史散文是以历史题材为主的散文，凡记述历史事件、历史人物的文章和书籍都是历史散文，如《左传》。\n两汉\n西汉时期的司马迁的《史记》把传记散文推到了前所未有的高峰。东汉以后，开始出现了书、记、碑、铭、论、序等个体单篇散文形式。司马相如、扬雄、班固、张衡四人被后世誉为汉赋四大家。\n唐宋\n在古文运动的推动下，散文的写法日益繁复，出现了文学散文，产生了不少优秀的山水游记、寓言、传记、杂文等作品，著名的“唐宋八大家”也在此时涌现。\n明代\n先有“七子”以拟古为主，后有唐宋派主张作品“皆自胸中流出”，较为有名的是归有光。\n清代散文：以桐城派为代表的清代散文，注重“义理”的体现。桐城派的代表作家姚鼐对我国古代散文文体加以总结，分为13类，包括论辩、序跋、奏议、书说、赠序、诏令、传状、碑志、杂说、箴铭、颂赞、辞赋、哀奠。\n近现代\n指与诗歌、小说、戏剧等并称的文学样式。特点是通过对现实生活中某些片段或生活事件的描述，表达作者的观点、感情，并揭示其社会意义，它可以在真人真事的基础上加工创造；不一定具有完整的故事情节和人物形象，而是着重于表现作者对生活的感受，具有选材、构思的灵活性和较强的抒情性。散文中的“我”通常是作者自己；语言不受韵律的限制，表达方式多样，可将叙述、议论、抒情、描写融为一体，也可以有所侧重；根据内容和主题的需要，可以像小说那样，通过对典型性的细节如生活片段，作形象描写、心理刻画、环境渲染、气氛烘托等，也可像诗歌那样运用象征等艺术手法，创设一定的艺术意境。散文的表现形式多种多样，杂文、短评、小品、随笔、速写、特写、游记、通讯、书信、日记、回忆录等都属于散文。总之，散文篇幅短小、形式自由、取材广泛、写法灵活、语言优美，能比较迅速地反映生活。")
	t.Logf("source data length:%v", len(msg))

	dl := len(msg) % 61

	message := make([]byte, len(msg))
	copy(message, msg)
	if dl > 0 {
		for i := 0; i < 61-dl; i++ {
			message = append(message, byte(0))
		}
	}

	a := len(message)
	dataShards := a / 61
	parityShards := a / 61 / 5

	enc, _ := New(dataShards, int(parityShards), WithCauchyMatrix(), WithLeopardGF16(false))
	data, _ := enc.Split(message)
	enc.Encode(data)
	ok, _ := enc.Verify(data)
	if !ok {
		t.Log("ok: false")
	}

	for i := 0; i < 1; i++ {
		//data[i] = nil
		data[len(data)-1-i] = nil
	}
	enc.Reconstruct(data)
	ok, err := enc.Verify(data)
	checkErr(err)
	buf := new(bytes.Buffer)
	enc.Join(buf, data, len(message))
	reMsg := buf.String()
	t.Logf("send data length:%v", lenBytes(data))
	t.Log(reMsg)
	t.Logf("result:%v", string(message) == reMsg)
}

func checkErr(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s", err.Error())
		os.Exit(2)
	}
}

func lenBytes(bs [][]byte) int {
	j := 0
	for i := range bs {
		j = j + len(bs[i])
	}
	return j
}

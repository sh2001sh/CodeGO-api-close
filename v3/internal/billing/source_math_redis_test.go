//go:build pgintegration

package billing

import (
	"context"
	"math/big"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

const sourceMathCalls = `
if ARGV[1] == 'mul' then return moneyMul(ARGV[2], ARGV[3]) end
if ARGV[1] == 'div' then
  local quotient, remainder = moneyDivmod(ARGV[2], ARGV[3])
  return {quotient, remainder}
end
return moneyMulDiv(ARGV[2], ARGV[3], ARGV[4], ARGV[5])
`

func sourceMathClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("V3_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("V3_TEST_REDIS_ADDR not set")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

func sourceMathEval(t *testing.T, rdb *redis.Client, args ...any) *redis.Cmd {
	t.Helper()
	callCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return rdb.Eval(callCtx, moneyLua+sourceMathLua+sourceMathCalls, nil, args...)
}

func TestSourceMathExactVectors(t *testing.T) {
	rdb := sourceMathClient(t)
	for _, tc := range []struct{ a, b, want string }{
		{"0", "9223372036854775807", "0"},
		{"000012", "000003", "36"},
		{"9007199254740993", "3", "27021597764222979"},
		{"9223372036854775807", "9223372036854775807", "85070591730234615847396907784232501249"},
		{"85070591730234615847396907784232501249", "9223372036854775807", "784637716923335095224261902710254454442933591094742482943"},
		{"99999999999999999999999999999999999999", "1000000000000000000", "99999999999999999999999999999999999999000000000000000000"},
	} {
		got, err := sourceMathEval(t, rdb, "mul", tc.a, tc.b).Text()
		if err != nil || got != tc.want {
			t.Fatalf("%s * %s = %s, want %s: %v", tc.a, tc.b, got, tc.want, err)
		}
	}
	for _, tc := range []struct{ a, b, quotient, remainder string }{
		{"0", "1", "0", "0"},
		{"000017", "000004", "4", "1"},
		{"3", "5", "0", "3"},
		{"9007199254740993", "2", "4503599627370496", "1"},
		{"85070591730234615847396907784232501249", "9223372036854775807", "9223372036854775807", "0"},
		{"85070591730234615847396907784232501250", "9223372036854775807", "9223372036854775807", "1"},
	} {
		got, err := sourceMathEval(t, rdb, "div", tc.a, tc.b).StringSlice()
		if err != nil || len(got) != 2 || got[0] != tc.quotient || got[1] != tc.remainder {
			t.Fatalf("%s / %s = %v, want [%s %s]: %v", tc.a, tc.b, got, tc.quotient, tc.remainder, err)
		}
	}
}

func TestSourceMathRationalRounding(t *testing.T) {
	rdb := sourceMathClient(t)
	for _, tc := range []struct{ a, b, denominator, floor, ceil, halfUp string }{
		{"0", "9", "1", "0", "0", "0"},
		{"1", "2", "3", "0", "1", "1"},
		{"1", "1", "3", "0", "1", "0"},
		{"1", "3", "2", "1", "2", "2"},
		{"1", "2", "2", "1", "1", "1"},
		{"9007199254740993", "3", "2", "13510798882111489", "13510798882111490", "13510798882111490"},
		{"9223372036854775807", "9223372036854775807", "9223372036854775806", "9223372036854775808", "9223372036854775809", "9223372036854775808"},
	} {
		for mode, want := range map[string]string{"floor": tc.floor, "ceil": tc.ceil, "half_up": tc.halfUp} {
			got, err := sourceMathEval(t, rdb, "ratio", tc.a, tc.b, tc.denominator, mode).Text()
			if err != nil || got != want {
				t.Fatalf("%s * %s / %s (%s) = %s, want %s: %v", tc.a, tc.b, tc.denominator, mode, got, want, err)
			}
		}
	}
}

func TestSourceMathMatchesIndependentBigIntArithmetic(t *testing.T) {
	rdb := sourceMathClient(t)
	rng := rand.New(rand.NewPCG(37, 91))
	digits := func(n int) string {
		var b strings.Builder
		for range n {
			b.WriteByte(byte('0' + rng.IntN(10)))
		}
		return b.String()
	}
	for range 90 {
		a, b, denominator := digits(38), digits(19), digits(1+rng.IntN(57))
		ai, _ := new(big.Int).SetString(a, 10)
		bi, _ := new(big.Int).SetString(b, 10)
		den, _ := new(big.Int).SetString(denominator, 10)
		den.Add(den, big.NewInt(1))
		product := new(big.Int).Mul(ai, bi)
		got, err := sourceMathEval(t, rdb, "mul", a, b).Text()
		if err != nil || got != product.String() {
			t.Fatalf("57-digit product mismatch: got=%s want=%s error=%v", got, product, err)
		}
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(product, den, remainder)
		divided, err := sourceMathEval(t, rdb, "div", product.String(), den.String()).StringSlice()
		if err != nil || len(divided) != 2 || divided[0] != quotient.String() || divided[1] != remainder.String() {
			t.Fatalf("long division mismatch: got=%v want=[%s %s] error=%v", divided, quotient, remainder, err)
		}
		for _, mode := range []string{"floor", "ceil", "half_up"} {
			want := new(big.Int).Set(quotient)
			if (mode == "ceil" && remainder.Sign() > 0) || (mode == "half_up" && new(big.Int).Lsh(remainder, 1).Cmp(den) >= 0) {
				want.Add(want, big.NewInt(1))
			}
			got, err := sourceMathEval(t, rdb, "ratio", a, b, den.String(), mode).Text()
			if err != nil || got != want.String() {
				t.Fatalf("ratio %s mismatch: got=%s want=%s error=%v", mode, got, want, err)
			}
		}
	}
}

func TestSourceMathRejectsInvalidNumbersAndDivisionByZero(t *testing.T) {
	rdb := sourceMathClient(t)
	for _, args := range [][]any{
		{"mul", "-1", "1"}, {"mul", "1", "1.5"}, {"mul", "", "1"},
		{"mul", " 1", "1"}, {"mul", "1e3", "1"}, {"mul", "+1", "1"},
		{"div", "1", "000"}, {"div", "-1", "2"},
		{"ratio", "1", "1", "0", "floor"},
		{"ratio", "0", "1", "0", "ceil"},
		{"ratio", "1", "1", "-1", "floor"},
		{"ratio", "1", "1", "2", "unknown"},
	} {
		if err := sourceMathEval(t, rdb, args...).Err(); err == nil {
			t.Fatalf("invalid arithmetic accepted: %v", args)
		}
	}
}

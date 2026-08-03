package textutil

import "testing"

func TestReverse(t *testing.T) {
	if Reverse("abc") != "cba" {
		t.Fatal("no")
	}
}

func TestCountWords(t *testing.T) {
	if CountWords("a b  c") != 3 {
		t.Fatal("no")
	}
}

func TestTitle(t *testing.T) {
	if Title("hello world") != "Hello World" {
		t.Fatal("no")
	}
}

func TestTrimAll(t *testing.T) {
	if TrimAll("--x--", "-") != "x" {
		t.Fatal("no")
	}
}

func TestIsPalindrome(t *testing.T) {
	if !IsPalindrome("Racecar") {
		t.Fatal("no")
	}
	if IsPalindrome("hello") {
		t.Fatal("no")
	}
}

func TestTruncate(t *testing.T) {
	if Truncate("hello", 3) != "hel..." {
		t.Fatal("no")
	}
	if Truncate("hi", 5) != "hi" {
		t.Fatal("no")
	}
}

func TestSum(t *testing.T) {
	if Sum([]int{1, 2, 3}) != 6 {
		t.Fatal("no")
	}
}

func TestMax(t *testing.T) {
	value, ok := Max([]int{3, 1, 2})
	if !ok || value != 3 {
		t.Fatal("no")
	}
	if _, ok := Max(nil); ok {
		t.Fatal("no")
	}
}

func TestMean(t *testing.T) {
	value, ok := Mean([]int{2, 4})
	if !ok || value != 3 {
		t.Fatal("no")
	}
}

func TestClamp(t *testing.T) {
	if Clamp(5, 0, 10) != 5 {
		t.Fatal("no")
	}
	if Clamp(-1, 0, 10) != 0 {
		t.Fatal("no")
	}
	if Clamp(11, 0, 10) != 10 {
		t.Fatal("no")
	}
}

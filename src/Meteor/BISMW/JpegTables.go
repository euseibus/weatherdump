package BISMW

var qTable = [64]int{
	16, 11, 10, 16, 24, 40, 51, 61,
	12, 12, 14, 19, 26, 58, 60, 55,
	14, 13, 16, 24, 40, 57, 69, 56,
	14, 17, 22, 29, 51, 87, 80, 62,
	18, 22, 37, 56, 68, 109, 103, 77,
	24, 35, 55, 64, 81, 104, 113, 92,
	49, 64, 78, 87, 103, 121, 120, 101,
	72, 92, 95, 98, 112, 100, 103, 99,
}

var zigzag = [64]int{
	0, 1, 5, 6, 14, 15, 27, 28,
	2, 4, 7, 13, 16, 26, 29, 42,
	3, 8, 12, 17, 25, 30, 41, 43,
	9, 11, 18, 24, 31, 40, 44, 53,
	10, 19, 23, 32, 39, 45, 52, 54,
	20, 22, 33, 38, 46, 51, 55, 60,
	21, 34, 37, 47, 50, 56, 59, 61,
	35, 36, 48, 49, 57, 58, 62, 63,
}

func getQuantizationTable(qf float32) []float64 {
	var table [64]float64
	var f float64
	if (qf > 20) && (qf < 50) {
		f = 5000 / float64(qf)
	}
	if (qf > 50) && (qf < 100) {
		f = 200 - (2 * float64(qf))
	}
	for x := 0; x < 64; x++ {
		table[x] = f * float64(qTable[x]) / 100.0
		if table[x] < 1 {
			table[x] = 1
		}
	}
	return table[:]
}

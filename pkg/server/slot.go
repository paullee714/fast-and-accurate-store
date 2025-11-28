package server

// CRC16 implementation (IBM/ANSI) for slot hashing, adapted for RESP cluster hashing.
func crc16(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func keySlot(key string, slots int) int {
	if slots <= 0 {
		return 0
	}
	return int(crc16([]byte(key)) % uint16(slots))
}

package buffer

func (b *Buffer) SubApplyBytes(offset, length int, fn func([]byte)) {
	for v := b.data.Front(); length > 0 && v != nil; v = v.Next() {
		if length <= 0 {
			break
		}

		if offset >= v.Size() {
			offset -= v.Size()
			continue
		}

		startPoint := 0
		if offset > 0 {
			startPoint = offset
			offset = 0
		}

		endPoint := 0
		if length < v.Size()-startPoint {
			endPoint = startPoint + length
		} else {
			endPoint = v.Size()
		}

		data := v.AsSlice()[startPoint:endPoint]

		fn(data)

		length -= len(data)
	}
}

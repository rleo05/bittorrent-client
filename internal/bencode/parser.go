package bencode

import (
	"bytes"
	"crypto/sha1"
	"fmt"
)

type Parser struct {
	data     []byte
	pos      int
	infoHash [20]byte
}

const MAX_STRING_SIZE = 10 * 1024 * 1024

func Parse(data []byte) (result any, hash [20]byte, err error) {
	parser := &Parser{data: data, pos: 0}

	result, err = parser.parseValue()
	if err != nil {
		return nil, [20]byte{}, fmt.Errorf("bencode parse error: %w", err)
	}

	if parser.pos < len(data) {
		return nil, [20]byte{}, fmt.Errorf("extra data after root element")
	}

	return result, parser.infoHash, nil
}

func (p *Parser) parseValue() (any, error) {
	currByte, err := p.peek()
	if err != nil {
		return nil, err
	}

	switch currByte {
	case 'd':
		return p.parseDictionary()
	case 'l':
		return p.parseList()
	case 'i':
		return p.parseInteger()
	default:
		return p.parseString()
	}
}

func (p *Parser) parseDictionary() (map[string]any, error) {
	p.next()

	dict := make(map[string]any, 8)
	var previousKey []byte

	for {
		b, err := p.peek()
		if err != nil {
			return nil, err
		}
		if b == 'e' {
			break
		}

		keyBytes, err := p.parseString()
		if err != nil {
			return nil, err
		}
		key := string(keyBytes)

		if previousKey != nil && bytes.Compare(previousKey, keyBytes) > 0 {
			return nil, fmt.Errorf("dictionary keys must be sorted")
		}

		startPos := p.pos
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}

		if key == "info" {
			endPos := p.pos
			rawInfoBytes := p.data[startPos:endPos]
			p.infoHash = sha1.Sum(rawInfoBytes)
		}

		dict[key] = value
		previousKey = keyBytes
	}

	p.next()

	return dict, nil
}

func (p *Parser) parseList() ([]any, error) {
	p.next()

	var list []any

	for {
		b, err := p.peek()
		if err != nil {
			return nil, err
		}
		if b == 'e' {
			break
		}

		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		list = append(list, val)
	}

	p.next()

	return list, nil
}

func (p *Parser) parseInteger() (int64, error) {
	p.next()
	value := int64(0)
	signal := int64(1)

	b, err := p.peek()
	if err != nil {
		return 0, err
	}

	if b == '-' {
		signal = int64(-1)
		p.next()
	}

	digitsCount := 0
	for {
		currByte, err := p.peek()
		if err != nil {
			return 0, err
		}
		if currByte == 'e' {
			break
		}

		if currByte < '0' || currByte > '9' {
			return 0, fmt.Errorf("malformed file: invalid digit")
		}

		if digitsCount == 0 && currByte == '0' {
			p.next()
			next, err := p.peek()
			if err != nil {
				return 0, err
			}
			if next != 'e' {
				return 0, fmt.Errorf("malformed file: leading zero")
			}
			digitsCount++
			continue
		}

		digit := int64(currByte - '0')
		value = value*10 + digit
		digitsCount++
		p.next()
	}

	p.next()

	if (value == 0 && signal < 0) || digitsCount == 0 {
		return 0, fmt.Errorf("malformed file")
	}

	return value * signal, nil
}

func (p *Parser) parseString() ([]byte, error) {
	totalBytes := 0

	for {
		currByte, err := p.peek()
		if err != nil {
			return nil, err
		}
		if currByte == ':' {
			break
		}

		if currByte < '0' || currByte > '9' {
			return nil, fmt.Errorf("malformed file: invalid digit")
		}

		if totalBytes == 0 && currByte == '0' {
			p.next()
			next, err := p.peek()
			if err != nil {
				return nil, err
			}
			if next != ':' {
				return nil, fmt.Errorf("malformed file: leading zero")
			}
			continue
		}

		digit := int(currByte - '0')

		if totalBytes > MAX_STRING_SIZE/10 {
			return nil, fmt.Errorf("string length overflow")
		}

		totalBytes = totalBytes*10 + digit

		if totalBytes > MAX_STRING_SIZE {
			return nil, fmt.Errorf("string too large")
		}
		p.next()
	}
	p.next()

	if p.pos+totalBytes > len(p.data) {
		return nil, fmt.Errorf("malformed file")
	}

	finalValue := p.data[p.pos : p.pos+totalBytes]
	p.pos += totalBytes
	return finalValue, nil
}

func (p *Parser) peek() (byte, error) {
	if p.pos >= len(p.data) {
		return 0, fmt.Errorf("malformed file: unexpected EOF")
	}
	return p.data[p.pos], nil
}

func (p *Parser) next() byte {
	b, _ := p.peek()
	p.pos++
	return b
}

package main

type StreamReadWriter struct {
	WriteFunc func(p []byte) error
	ReadFunc  func() ([]byte, error)
}

func (s *StreamReadWriter) Write(p []byte) (n int, err error) {
	err = s.WriteFunc(p)
	n = len(p)
	return
}

func (s StreamReadWriter) Read(p []byte) (n int, err error) {
	data, err := s.ReadFunc()
	n = copy(p, data)
	data = data[n:]
	if len(data) == 0 {
		return n, err
	}
	return
}

type StreamWriter struct {
	WriteFunc func(p []byte) error
}

func (s *StreamWriter) Write(p []byte) (n int, err error) {
	err = s.WriteFunc(p)
	n = len(p)
	return
}

type StreamReader struct {
	ReadFunc func() ([]byte, error)
}

func (s StreamReader) Read(p []byte) (n int, err error) {
	data, err := s.ReadFunc()
	n = copy(p, data)
	data = data[n:]
	if len(data) == 0 {
		return n, err
	}
	return
}

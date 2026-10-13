package main

import (
	"bytes"
	"fmt"
	"io"
	"reflect"

	"traitdeclare/adapters"
	"traitdeclare/other"
)

func main() {
	buffer := bytes.NewBufferString("source")
	reader := adapters.AsBytesBufferReader(buffer)
	sized := adapters.ForBytesBuffer(buffer).AsSized()
	_, sourceWriterTo := any(buffer).(io.WriterTo)
	_, viewWriterTo := any(reader).(io.WriterTo)
	_, sourceTrait := any(buffer).(adapters.Reader)
	fmt.Println(sourceWriterTo, viewWriterTo, sourceTrait, sized.Len())
	var output bytes.Buffer
	n, err := io.Copy(&output, reader)
	fmt.Println(n, output.String(), err, sized.Len())
	buffer.WriteString("pair")
	ptrReader := adapters.AsBytesBufferPtrReader(buffer)
	chunk := make([]byte, 2)
	nRead, readErr := ptrReader.Read(chunk)
	fmt.Println(nRead, string(chunk), readErr, sized.Len())
	nRead, readErr = adapters.CopyRead(buffer, chunk)
	fmt.Println(nRead, string(chunk), readErr, sized.Len())
	otherBuffer := bytes.NewBufferString("other")
	otherReader := other.AsBytesBufferReader(otherBuffer)
	data, err := io.ReadAll(otherReader)
	fmt.Println(string(data), err)
	otherBuffer.WriteString("again")
	data, err = io.ReadAll(other.ForBytesBuffer(otherBuffer).AsReader())
	fmt.Println(string(data), err)
	otherBuffer.WriteString("view")
	otherView := other.View(otherBuffer)
	data, err = io.ReadAll(otherView)
	fmt.Println(string(data), err, reflect.TypeOf(otherView) == reflect.TypeOf(otherReader), reflect.TypeOf(otherReader) != reflect.TypeOf(reader))
}

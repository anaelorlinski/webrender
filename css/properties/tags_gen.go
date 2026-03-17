package properties

import "fmt"

// Code generated from properties/gen/gen.go DO NOT EDIT

func NewTag(s string) Tag {
	switch s {
	case "auto":
		return 1
	case "none":
		return 2
	case "span":
		return 3
	case "subgrid":
		return 4
	case "attr":
		return 5
	case "internal":
		return 6
	case "external":
		return 7
	case "local":
		return 8
	case "attachment":
		return 9
	case "content":
		return 10
	case "from-font":
		return 11
	case "fill":
		return 12
	case "min-content":
		return 13
	case "max-content":
		return 14
	case "normal":
		return 15
	case "cover":
		return 16
	case "contain":
		return 17
	case "xx-small":
		return 18
	case "x-small":
		return 19
	case "small":
		return 20
	case "medium":
		return 21
	case "large":
		return 22
	case "x-large":
		return 23
	case "xx-large":
		return 24
	case "smaller":
		return 25
	case "larger":
		return 26
	case "thin":
		return 27
	case "thick":
		return 28
	case "baseline":
		return 29
	case "middle":
		return 30
	case "text-top":
		return 31
	case "text-bottom":
		return 32
	case "top":
		return 33
	case "bottom":
		return 34
	case "super":
		return 35
	case "sub":
		return 36
	default:
		return 0
	}
}

func (t Tag) String() string {
	switch t {
	case 1:
		return "auto"
	case 2:
		return "none"
	case 3:
		return "span"
	case 4:
		return "subgrid"
	case 5:
		return "attr"
	case 6:
		return "internal"
	case 7:
		return "external"
	case 8:
		return "local"
	case 9:
		return "attachment"
	case 10:
		return "content"
	case 11:
		return "from-font"
	case 12:
		return "fill"
	case 13:
		return "min-content"
	case 14:
		return "max-content"
	case 15:
		return "normal"
	case 16:
		return "cover"
	case 17:
		return "contain"
	case 18:
		return "xx-small"
	case 19:
		return "x-small"
	case 20:
		return "small"
	case 21:
		return "medium"
	case 22:
		return "large"
	case 23:
		return "x-large"
	case 24:
		return "xx-large"
	case 25:
		return "smaller"
	case 26:
		return "larger"
	case 27:
		return "thin"
	case 28:
		return "thick"
	case 29:
		return "baseline"
	case 30:
		return "middle"
	case 31:
		return "text-top"
	case 32:
		return "text-bottom"
	case 33:
		return "top"
	case 34:
		return "bottom"
	case 35:
		return "super"
	case 36:
		return "sub"
	default:
		return fmt.Sprintf("unknown tag %d", t)
	}
}

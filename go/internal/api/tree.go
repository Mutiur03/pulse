package api

import (
	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

func strPtr(s string) *string { return apikit.StrPtr(s) }

func errorOccurrence(e ch.ErrorRecord) gen.ErrorOccurrence { return apikit.ErrorOccurrence(e) }

func productEvent(e ch.EventRecord) gen.ProductEvent { return apikit.ProductEvent(e) }

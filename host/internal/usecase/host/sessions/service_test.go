package sessions

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

type ServiceSuite struct {
	suite.Suite
	repository *MockRepository
	ids        *MockIDGenerator
	clock      *MockClock
	pricing    *MockPricingCatalog
}

// TestServiceSuite runs active-session persistence, projection, replacement, and failure scenarios.
func TestServiceSuite(t *testing.T) {
	t.Parallel()

	// Arrange a fresh active-session service suite.

	// Act by running every suite scenario.

	// Assert through the scenario assertions and mock expectations owned by the suite.
	suite.Run(t, new(ServiceSuite))
}

func (s *ServiceSuite) SetupTest() {
	controller := gomock.NewController(s.T())
	s.repository = NewMockRepository(controller)
	s.ids = NewMockIDGenerator(controller)
	s.clock = NewMockClock(controller)
	s.pricing = NewMockPricingCatalog(controller)
	s.pricing.EXPECT().Pricing(gomock.Any(), gomock.Any()).Return(mo.None[model.Pricing]()).AnyTimes()
}

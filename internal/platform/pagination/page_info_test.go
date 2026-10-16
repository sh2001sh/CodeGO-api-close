package pagination

import (
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetPageQueryRejectsNonPositivePageSizes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{
		"?page_size=-1",
		"?page_size=-100&ps=-20&size=-30",
		"?page_size=0&ps=-20&size=0",
	} {
		t.Run(query, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest("GET", "/"+query, nil)

			page := GetPageQuery(context)

			require.Equal(t, defaultItemsPerPage, page.PageSize)
			require.Equal(t, 1, page.Page)
		})
	}
}

func TestGetPageQueryNormalizesInvalidPagesAndAvoidsOffsetOverflow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, value := range []string{"-1", "-100", "0", "invalid", "999999999999999999999999999"} {
		t.Run(value, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest("GET", "/?p="+value, nil)
			page := GetPageQuery(context)
			require.Equal(t, 1, page.Page)
			require.Zero(t, page.GetStartIdx())
		})
	}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/?p="+strconv.Itoa(int(^uint(0)>>1))+"&page_size=100", nil)
	page := GetPageQuery(context)
	require.Positive(t, page.GetStartIdx())
	require.Greater(t, page.GetEndIdx(), page.GetStartIdx())
}

func TestGetPageQueryKeepsPositiveFallbackAndCapsMaximum(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/?page_size=-1&ps=25", nil)
	require.Equal(t, 25, GetPageQuery(context).PageSize)

	context, _ = gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/?page_size=1000", nil)
	require.Equal(t, maxItemsPerPage, GetPageQuery(context).PageSize)
}

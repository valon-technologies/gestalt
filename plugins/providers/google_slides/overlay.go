package google_slides

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
)

const (
	overlayProviderName        = "google_slides"
	overlayProviderDisplayName = "Google Slides"
	overlayProviderDescription = "Google Slides overlay operations using batchUpdate API"
	slidesAPIBase              = "https://slides.googleapis.com/v1"

	opCreatePresentation         = "create_presentation"
	opCreateSlide                = "create_slide"
	opDeleteObject               = "delete_object"
	opDuplicateObject            = "duplicate_object"
	opUpdateSlidesPosition       = "update_slides_position"
	opCreateShape                = "create_shape"
	opInsertText                 = "insert_text"
	opDeleteText                 = "delete_text"
	opCreateImage                = "create_image"
	opCreateTable                = "create_table"
	opCreateLine                 = "create_line"
	opReplaceAllText             = "replace_all_text"
	opReplaceAllShapesWithImage  = "replace_all_shapes_with_image"
	opUpdateTextStyle            = "update_text_style"
	opUpdateShapeProperties      = "update_shape_properties"
	opUpdatePageProperties       = "update_page_properties"
	opUpdateParagraphStyle       = "update_paragraph_style"
	opCreateParagraphBullets     = "create_paragraph_bullets"
	opUpdatePageElementTransform = "update_page_element_transform"
	opUpdatePageElementsZOrder   = "update_page_elements_z_order"
	opGroupObjects               = "group_objects"
	opUngroupObjects             = "ungroup_objects"
	opUpdateTableCellProperties  = "update_table_cell_properties"
	opCreateStyledTextBox        = "create_styled_text_box"
	opCreateBulletList           = "create_bullet_list"
	opCreateTitleSlide           = "create_title_slide"

	paramPresentationID       = "presentation_id"
	paramTitle                = "title"
	paramInsertionIndex       = "insertion_index"
	paramLayout               = "layout"
	paramObjectID             = "object_id"
	paramPageObjectID         = "page_object_id"
	paramShapeType            = "shape_type"
	paramX                    = "x"
	paramY                    = "y"
	paramWidth                = "width"
	paramHeight               = "height"
	paramText                 = "text"
	paramSlideObjectIDs       = "slide_object_ids"
	paramStartIndex           = "start_index"
	paramEndIndex             = "end_index"
	paramRangeType            = "type"
	paramURL                  = "url"
	paramRows                 = "rows"
	paramColumns              = "columns"
	paramFind                 = "find"
	paramReplacement          = "replacement"
	paramMatchCase            = "match_case"
	paramImageURL             = "image_url"
	paramReplaceMethod        = "replace_method"
	paramBold                 = "bold"
	paramItalic               = "italic"
	paramUnderline            = "underline"
	paramFontFamily           = "font_family"
	paramFontSize             = "font_size"
	paramForegroundColor      = "foreground_color"
	paramBackgroundColor      = "background_color"
	paramLinkURL              = "link_url"
	paramFillColor            = "fill_color"
	paramOutlineColor         = "outline_color"
	paramOutlineWeight        = "outline_weight"
	paramAlignment            = "alignment"
	paramLineSpacing          = "line_spacing"
	paramSpaceAbove           = "space_above"
	paramSpaceBelow           = "space_below"
	paramLineCategory         = "line_category"
	paramStartX               = "start_x"
	paramStartY               = "start_y"
	paramEndX                 = "end_x"
	paramEndY                 = "end_y"
	paramBulletPreset         = "bullet_preset"
	paramTranslateX           = "translate_x"
	paramTranslateY           = "translate_y"
	paramScaleX               = "scale_x"
	paramScaleY               = "scale_y"
	paramApplyMode            = "apply_mode"
	paramPageElementObjectIDs = "page_element_object_ids"
	paramOperation            = "operation"
	paramChildrenObjectIDs    = "children_object_ids"
	paramGroupObjectID        = "group_object_id"
	paramObjectIDs            = "object_ids"
	paramRowIndex             = "row_index"
	paramColumnIndex          = "column_index"
	paramPaddingTop           = "padding_top"
	paramPaddingBottom        = "padding_bottom"
	paramPaddingLeft          = "padding_left"
	paramPaddingRight         = "padding_right"
	paramItems                = "items"
	paramSubtitle             = "subtitle"
	paramTitleColor           = "title_color"
	paramSubtitleColor        = "subtitle_color"
	paramTitleFont            = "title_font"
	paramTitleSize            = "title_size"
	paramSubtitleFont         = "subtitle_font"
	paramSubtitleSize         = "subtitle_size"

	contentTypeJSON = "application/json"
)

var overlayOps = []core.Operation{
	{Name: opCreatePresentation, Description: "Create a new Google Slides presentation", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramTitle, Type: "string", Required: true, Description: "Presentation title"},
		}},
	{Name: opCreateSlide, Description: "Create a new slide in a presentation", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramInsertionIndex, Type: "integer", Description: "Position to insert slide"},
			{Name: paramLayout, Type: "string", Description: "Predefined layout"},
			{Name: paramObjectID, Type: "string", Description: "Custom object ID for the slide"},
		}},
	{Name: opDeleteObject, Description: "Delete an object from a presentation", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramObjectID, Type: "string", Required: true, Description: "Object ID to delete"},
		}},
	{Name: opDuplicateObject, Description: "Duplicate a slide or element", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramObjectID, Type: "string", Required: true, Description: "Object ID to duplicate"},
		}},
	{Name: opUpdateSlidesPosition, Description: "Reorder slides in a presentation", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramSlideObjectIDs, Type: "array", Required: true, Description: "List of slide IDs to move"},
			{Name: paramInsertionIndex, Type: "integer", Required: true, Description: "Target position"},
		}},
	{Name: opCreateShape, Description: "Create a shape on a slide", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramPageObjectID, Type: "string", Required: true, Description: "Target slide ID"},
			{Name: paramShapeType, Type: "string", Required: true, Description: "Shape type: TEXT_BOX, RECTANGLE, ELLIPSE, etc."},
			{Name: paramObjectID, Type: "string", Description: "Custom object ID"},
			{Name: paramX, Type: "number", Description: "X position in points"},
			{Name: paramY, Type: "number", Description: "Y position in points"},
			{Name: paramWidth, Type: "number", Description: "Width in points"},
			{Name: paramHeight, Type: "number", Description: "Height in points"},
		}},
	{Name: opInsertText, Description: "Insert text into a shape", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramObjectID, Type: "string", Required: true, Description: "Target shape ID"},
			{Name: paramText, Type: "string", Required: true, Description: "Text to insert"},
			{Name: paramInsertionIndex, Type: "integer", Description: "Character position to insert at"},
		}},
	{Name: opDeleteText, Description: "Delete text from a shape", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramObjectID, Type: "string", Required: true, Description: "Target shape ID"},
			{Name: paramStartIndex, Type: "integer", Description: "Start character index"},
			{Name: paramEndIndex, Type: "integer", Description: "End character index"},
			{Name: paramRangeType, Type: "string", Description: "Range type: ALL, FROM_START_INDEX, FIXED_RANGE"},
		}},
	{Name: opCreateImage, Description: "Insert an image into a slide", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramPageObjectID, Type: "string", Required: true, Description: "Target slide ID"},
			{Name: paramURL, Type: "string", Required: true, Description: "Public image URL"},
			{Name: paramObjectID, Type: "string", Description: "Custom object ID"},
			{Name: paramX, Type: "number", Description: "X position in points"},
			{Name: paramY, Type: "number", Description: "Y position in points"},
			{Name: paramWidth, Type: "number", Description: "Width in points"},
			{Name: paramHeight, Type: "number", Description: "Height in points"},
		}},
	{Name: opCreateTable, Description: "Create a table on a slide", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramPageObjectID, Type: "string", Required: true, Description: "Target slide ID"},
			{Name: paramRows, Type: "integer", Required: true, Description: "Number of rows"},
			{Name: paramColumns, Type: "integer", Required: true, Description: "Number of columns"},
			{Name: paramObjectID, Type: "string", Description: "Custom object ID"},
		}},
	{Name: opCreateLine, Description: "Create a line on a slide", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramPageObjectID, Type: "string", Required: true, Description: "Target slide ID"},
			{Name: paramLineCategory, Type: "string", Description: "Line type: STRAIGHT, BENT, or CURVED"},
			{Name: paramObjectID, Type: "string", Description: "Custom object ID"},
			{Name: paramStartX, Type: "number", Description: "Start X position in points"},
			{Name: paramStartY, Type: "number", Description: "Start Y position in points"},
			{Name: paramEndX, Type: "number", Description: "End X position in points"},
			{Name: paramEndY, Type: "number", Description: "End Y position in points"},
		}},
	{Name: opReplaceAllText, Description: "Find and replace text throughout a presentation", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramFind, Type: "string", Required: true, Description: "Text to find"},
			{Name: paramReplacement, Type: "string", Required: true, Description: "Replacement text"},
			{Name: paramMatchCase, Type: "boolean", Description: "Case-sensitive match"},
		}},
	{Name: opReplaceAllShapesWithImage, Description: "Replace shapes containing text with an image", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramFind, Type: "string", Required: true, Description: "Text to find in shapes"},
			{Name: paramImageURL, Type: "string", Required: true, Description: "URL of replacement image"},
			{Name: paramReplaceMethod, Type: "string", Description: "Replace method: CENTER_INSIDE or CENTER_CROP"},
		}},
	{Name: opUpdateTextStyle, Description: "Update text styling", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramObjectID, Type: "string", Required: true, Description: "Shape ID containing the text"},
			{Name: paramStartIndex, Type: "integer", Description: "Start character index"},
			{Name: paramEndIndex, Type: "integer", Description: "End character index"},
			{Name: paramBold, Type: "boolean", Description: "Make text bold"},
			{Name: paramItalic, Type: "boolean", Description: "Make text italic"},
			{Name: paramUnderline, Type: "boolean", Description: "Underline text"},
			{Name: paramFontFamily, Type: "string", Description: "Font name"},
			{Name: paramFontSize, Type: "number", Description: "Font size in points"},
			{Name: paramForegroundColor, Type: "string", Description: "Text color as hex #RRGGBB"},
			{Name: paramBackgroundColor, Type: "string", Description: "Text background color as hex #RRGGBB"},
			{Name: paramLinkURL, Type: "string", Description: "Make text a hyperlink"},
		}},
	{Name: opUpdateShapeProperties, Description: "Update shape properties", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramObjectID, Type: "string", Required: true, Description: "Shape ID"},
			{Name: paramFillColor, Type: "string", Description: "Fill color as hex #RRGGBB"},
			{Name: paramOutlineColor, Type: "string", Description: "Outline color as hex #RRGGBB"},
			{Name: paramOutlineWeight, Type: "number", Description: "Outline thickness in points"},
		}},
	{Name: opUpdatePageProperties, Description: "Update page/slide properties", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramObjectID, Type: "string", Required: true, Description: "Page/slide ID"},
			{Name: paramBackgroundColor, Type: "string", Description: "Background color as hex #RRGGBB"},
		}},
	{Name: opUpdateParagraphStyle, Description: "Update paragraph styling", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramObjectID, Type: "string", Required: true, Description: "Shape ID containing the text"},
			{Name: paramAlignment, Type: "string", Description: "Text alignment: START, CENTER, END, JUSTIFIED"},
			{Name: paramLineSpacing, Type: "number", Description: "Line spacing percentage"},
			{Name: paramSpaceAbove, Type: "number", Description: "Space above paragraph in points"},
			{Name: paramSpaceBelow, Type: "number", Description: "Space below paragraph in points"},
		}},
	{Name: opCreateParagraphBullets, Description: "Add bullet points or numbering to text", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramObjectID, Type: "string", Required: true, Description: "Shape ID containing the text"},
			{Name: paramBulletPreset, Type: "string", Description: "Bullet style preset"},
			{Name: paramStartIndex, Type: "integer", Description: "Start character index"},
			{Name: paramEndIndex, Type: "integer", Description: "End character index"},
		}},
	{Name: opUpdatePageElementTransform, Description: "Update position and size of a page element", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramObjectID, Type: "string", Required: true, Description: "Element ID to transform"},
			{Name: paramTranslateX, Type: "number", Description: "New X position in points"},
			{Name: paramTranslateY, Type: "number", Description: "New Y position in points"},
			{Name: paramScaleX, Type: "number", Description: "Horizontal scale factor"},
			{Name: paramScaleY, Type: "number", Description: "Vertical scale factor"},
			{Name: paramApplyMode, Type: "string", Description: "Apply mode: RELATIVE or ABSOLUTE"},
		}},
	{Name: opUpdatePageElementsZOrder, Description: "Change the z-order of page elements", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramPageElementObjectIDs, Type: "array", Required: true, Description: "List of element IDs to reorder"},
			{Name: paramOperation, Type: "string", Required: true, Description: "Z-order operation: BRING_TO_FRONT, BRING_FORWARD, SEND_BACKWARD, SEND_TO_BACK"},
		}},
	{Name: opGroupObjects, Description: "Group multiple page elements together", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramChildrenObjectIDs, Type: "array", Required: true, Description: "List of element IDs to group (minimum 2)"},
			{Name: paramGroupObjectID, Type: "string", Description: "Custom ID for the new group"},
		}},
	{Name: opUngroupObjects, Description: "Ungroup a group element into its parts", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramObjectIDs, Type: "array", Required: true, Description: "List of group IDs to ungroup"},
		}},
	{Name: opUpdateTableCellProperties, Description: "Update table cell properties", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramObjectID, Type: "string", Required: true, Description: "Table ID"},
			{Name: paramRowIndex, Type: "integer", Required: true, Description: "Row index (0-based)"},
			{Name: paramColumnIndex, Type: "integer", Required: true, Description: "Column index (0-based)"},
			{Name: paramBackgroundColor, Type: "string", Description: "Cell background color as hex #RRGGBB"},
			{Name: paramPaddingTop, Type: "number", Description: "Top padding in points"},
			{Name: paramPaddingBottom, Type: "number", Description: "Bottom padding in points"},
			{Name: paramPaddingLeft, Type: "number", Description: "Left padding in points"},
			{Name: paramPaddingRight, Type: "number", Description: "Right padding in points"},
		}},
	{Name: opCreateStyledTextBox, Description: "Create a text box with text and styling in a single operation", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramPageObjectID, Type: "string", Required: true, Description: "Target slide ID"},
			{Name: paramText, Type: "string", Required: true, Description: "Text content"},
			{Name: paramX, Type: "number", Description: "X position in points"},
			{Name: paramY, Type: "number", Description: "Y position in points"},
			{Name: paramWidth, Type: "number", Description: "Width in points"},
			{Name: paramHeight, Type: "number", Description: "Height in points"},
			{Name: paramObjectID, Type: "string", Description: "Custom object ID"},
			{Name: paramFontFamily, Type: "string", Description: "Font name"},
			{Name: paramFontSize, Type: "number", Description: "Font size in points"},
			{Name: paramBold, Type: "boolean", Description: "Make text bold"},
			{Name: paramItalic, Type: "boolean", Description: "Make text italic"},
			{Name: paramForegroundColor, Type: "string", Description: "Text color as hex #RRGGBB"},
			{Name: paramFillColor, Type: "string", Description: "Background fill color as hex #RRGGBB"},
			{Name: paramAlignment, Type: "string", Description: "Text alignment: START, CENTER, END, JUSTIFIED"},
		}},
	{Name: opCreateBulletList, Description: "Create a text box with bullet points in a single operation", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramPageObjectID, Type: "string", Required: true, Description: "Target slide ID"},
			{Name: paramItems, Type: "array", Required: true, Description: "List of bullet point strings"},
			{Name: paramX, Type: "number", Description: "X position in points"},
			{Name: paramY, Type: "number", Description: "Y position in points"},
			{Name: paramWidth, Type: "number", Description: "Width in points"},
			{Name: paramHeight, Type: "number", Description: "Height in points"},
			{Name: paramObjectID, Type: "string", Description: "Custom object ID"},
			{Name: paramBulletPreset, Type: "string", Description: "Bullet style"},
			{Name: paramFontFamily, Type: "string", Description: "Font name"},
			{Name: paramFontSize, Type: "number", Description: "Font size in points"},
			{Name: paramForegroundColor, Type: "string", Description: "Text color as hex #RRGGBB"},
		}},
	{Name: opCreateTitleSlide, Description: "Create a complete title slide with title, subtitle, and styling", Method: http.MethodPost,
		Parameters: []core.Parameter{
			{Name: paramPresentationID, Type: "string", Required: true, Description: "Presentation ID"},
			{Name: paramTitle, Type: "string", Required: true, Description: "Title text"},
			{Name: paramSubtitle, Type: "string", Description: "Subtitle text"},
			{Name: paramBackgroundColor, Type: "string", Description: "Slide background color as hex #RRGGBB"},
			{Name: paramTitleColor, Type: "string", Description: "Title text color as hex #RRGGBB"},
			{Name: paramSubtitleColor, Type: "string", Description: "Subtitle text color as hex #RRGGBB"},
			{Name: paramTitleFont, Type: "string", Description: "Title font family"},
			{Name: paramTitleSize, Type: "number", Description: "Title font size in points"},
			{Name: paramSubtitleFont, Type: "string", Description: "Subtitle font family"},
			{Name: paramSubtitleSize, Type: "number", Description: "Subtitle font size in points"},
			{Name: paramInsertionIndex, Type: "integer", Description: "Position to insert slide"},
		}},
}

var _ core.Provider = (*OverlayProvider)(nil)
var _ core.CatalogProvider = (*OverlayProvider)(nil)

type OverlayProvider struct {
	client *http.Client
}

func NewOverlayProvider() *OverlayProvider {
	return &OverlayProvider{client: http.DefaultClient}
}

func (p *OverlayProvider) Name() string                        { return overlayProviderName }
func (p *OverlayProvider) DisplayName() string                 { return overlayProviderDisplayName }
func (p *OverlayProvider) Description() string                 { return overlayProviderDescription }
func (p *OverlayProvider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeUser }
func (p *OverlayProvider) ListOperations() []core.Operation    { return overlayOps }

func (p *OverlayProvider) Catalog() *catalog.Catalog {
	ops := make([]catalog.CatalogOperation, len(overlayOps))
	for i, op := range overlayOps {
		params := make([]catalog.CatalogParameter, len(op.Parameters))
		for j, param := range op.Parameters {
			params[j] = catalog.CatalogParameter{
				Name:        param.Name,
				Type:        param.Type,
				Description: param.Description,
				Required:    param.Required,
			}
		}
		ops[i] = catalog.CatalogOperation{
			ID:          op.Name,
			Method:      op.Method,
			Description: op.Description,
			Parameters:  params,
		}
	}
	return &catalog.Catalog{
		Name:        overlayProviderName,
		DisplayName: overlayProviderDisplayName,
		Description: overlayProviderDescription,
		Operations:  ops,
	}
}

func (p *OverlayProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	switch operation {
	case opCreatePresentation:
		return p.createPresentation(ctx, params, token)
	case opCreateSlide:
		return p.executeBatchOp(ctx, params, token, p.buildCreateSlide)
	case opDeleteObject:
		return p.executeBatchOp(ctx, params, token, p.buildDeleteObject)
	case opDuplicateObject:
		return p.executeBatchOp(ctx, params, token, p.buildDuplicateObject)
	case opUpdateSlidesPosition:
		return p.executeBatchOp(ctx, params, token, p.buildUpdateSlidesPosition)
	case opCreateShape:
		return p.executeBatchOp(ctx, params, token, p.buildCreateShape)
	case opInsertText:
		return p.executeBatchOp(ctx, params, token, p.buildInsertText)
	case opDeleteText:
		return p.executeBatchOp(ctx, params, token, p.buildDeleteText)
	case opCreateImage:
		return p.executeBatchOp(ctx, params, token, p.buildCreateImage)
	case opCreateTable:
		return p.executeBatchOp(ctx, params, token, p.buildCreateTable)
	case opCreateLine:
		return p.executeBatchOp(ctx, params, token, p.buildCreateLine)
	case opReplaceAllText:
		return p.executeBatchOp(ctx, params, token, p.buildReplaceAllText)
	case opReplaceAllShapesWithImage:
		return p.executeBatchOp(ctx, params, token, p.buildReplaceAllShapesWithImage)
	case opUpdateTextStyle:
		return p.executeBatchOp(ctx, params, token, p.buildUpdateTextStyle)
	case opUpdateShapeProperties:
		return p.executeBatchOp(ctx, params, token, p.buildUpdateShapeProperties)
	case opUpdatePageProperties:
		return p.executeBatchOp(ctx, params, token, p.buildUpdatePageProperties)
	case opUpdateParagraphStyle:
		return p.executeBatchOp(ctx, params, token, p.buildUpdateParagraphStyle)
	case opCreateParagraphBullets:
		return p.executeBatchOp(ctx, params, token, p.buildCreateParagraphBullets)
	case opUpdatePageElementTransform:
		return p.executeBatchOp(ctx, params, token, p.buildUpdatePageElementTransform)
	case opUpdatePageElementsZOrder:
		return p.executeBatchOp(ctx, params, token, p.buildUpdatePageElementsZOrder)
	case opGroupObjects:
		return p.executeBatchOp(ctx, params, token, p.buildGroupObjects)
	case opUngroupObjects:
		return p.executeBatchOp(ctx, params, token, p.buildUngroupObjects)
	case opUpdateTableCellProperties:
		return p.executeBatchOp(ctx, params, token, p.buildUpdateTableCellProperties)
	case opCreateStyledTextBox:
		return p.executeBatchOp(ctx, params, token, p.buildCreateStyledTextBox)
	case opCreateBulletList:
		return p.executeBatchOp(ctx, params, token, p.buildCreateBulletList)
	case opCreateTitleSlide:
		return p.executeBatchOp(ctx, params, token, p.buildCreateTitleSlide)
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
}

type batchBuilder func(params map[string]any) (string, []map[string]any, error)

func (p *OverlayProvider) executeBatchOp(ctx context.Context, params map[string]any, token string, builder batchBuilder) (*core.OperationResult, error) {
	presID, reqs, err := builder(params)
	if err != nil {
		return nil, err
	}
	return p.batchUpdate(ctx, presID, reqs, token)
}

func (p *OverlayProvider) createPresentation(ctx context.Context, params map[string]any, token string) (*core.OperationResult, error) {
	title := strParam(params, paramTitle)
	if title == "" {
		return nil, fmt.Errorf("%s is required", paramTitle)
	}
	payload, _ := json.Marshal(map[string]string{"title": title})
	return p.doPost(ctx, slidesAPIBase+"/presentations", payload, token)
}

func (p *OverlayProvider) buildCreateSlide(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	if presID == "" {
		return "", nil, fmt.Errorf("%s is required", paramPresentationID)
	}
	req := map[string]any{}
	if v := strParam(params, paramObjectID); v != "" {
		req["objectId"] = v
	}
	if v, ok := intParamOk(params, paramInsertionIndex); ok {
		req["insertionIndex"] = v
	}
	if v := strParam(params, paramLayout); v != "" {
		req["slideLayoutReference"] = map[string]string{"predefinedLayout": v}
	}
	return presID, []map[string]any{{"createSlide": req}}, nil
}

func (p *OverlayProvider) buildDeleteObject(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	objID := strParam(params, paramObjectID)
	if presID == "" || objID == "" {
		return "", nil, fmt.Errorf("%s and %s are required", paramPresentationID, paramObjectID)
	}
	return presID, []map[string]any{{"deleteObject": map[string]any{"objectId": objID}}}, nil
}

func (p *OverlayProvider) buildDuplicateObject(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	objID := strParam(params, paramObjectID)
	if presID == "" || objID == "" {
		return "", nil, fmt.Errorf("%s and %s are required", paramPresentationID, paramObjectID)
	}
	return presID, []map[string]any{{"duplicateObject": map[string]any{"objectId": objID}}}, nil
}

func (p *OverlayProvider) buildUpdateSlidesPosition(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	if presID == "" {
		return "", nil, fmt.Errorf("%s is required", paramPresentationID)
	}
	slideIDs := strSliceParam(params, paramSlideObjectIDs)
	idx, ok := intParamOk(params, paramInsertionIndex)
	if !ok || len(slideIDs) == 0 {
		return "", nil, fmt.Errorf("%s and %s are required", paramSlideObjectIDs, paramInsertionIndex)
	}
	return presID, []map[string]any{{"updateSlidesPosition": map[string]any{
		"slideObjectIds": slideIDs,
		"insertionIndex": idx,
	}}}, nil
}

func (p *OverlayProvider) buildCreateShape(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	pageID := strParam(params, paramPageObjectID)
	shapeType := strParam(params, paramShapeType)
	if presID == "" || pageID == "" || shapeType == "" {
		return "", nil, fmt.Errorf("%s, %s, and %s are required", paramPresentationID, paramPageObjectID, paramShapeType)
	}
	req := map[string]any{
		"shapeType":         shapeType,
		"elementProperties": elementProperties(pageID, params, 100, 100, 300, 100),
	}
	if v := strParam(params, paramObjectID); v != "" {
		req["objectId"] = v
	}
	return presID, []map[string]any{{"createShape": req}}, nil
}

func (p *OverlayProvider) buildInsertText(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	objID := strParam(params, paramObjectID)
	text := strParam(params, paramText)
	if presID == "" || objID == "" || text == "" {
		return "", nil, fmt.Errorf("%s, %s, and %s are required", paramPresentationID, paramObjectID, paramText)
	}
	idx, _ := intParamOk(params, paramInsertionIndex)
	return presID, []map[string]any{{"insertText": map[string]any{
		"objectId":       objID,
		"text":           text,
		"insertionIndex": idx,
	}}}, nil
}

func (p *OverlayProvider) buildDeleteText(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	objID := strParam(params, paramObjectID)
	if presID == "" || objID == "" {
		return "", nil, fmt.Errorf("%s and %s are required", paramPresentationID, paramObjectID)
	}
	tr := map[string]any{"type": strParamDefault(params, paramRangeType, "ALL")}
	if v, ok := intParamOk(params, paramStartIndex); ok {
		tr["startIndex"] = v
	}
	if v, ok := intParamOk(params, paramEndIndex); ok {
		tr["endIndex"] = v
	}
	return presID, []map[string]any{{"deleteText": map[string]any{
		"objectId":  objID,
		"textRange": tr,
	}}}, nil
}

func (p *OverlayProvider) buildCreateImage(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	pageID := strParam(params, paramPageObjectID)
	url := strParam(params, paramURL)
	if presID == "" || pageID == "" || url == "" {
		return "", nil, fmt.Errorf("%s, %s, and %s are required", paramPresentationID, paramPageObjectID, paramURL)
	}
	req := map[string]any{
		"url":               url,
		"elementProperties": elementProperties(pageID, params, 50, 50, 400, 300),
	}
	if v := strParam(params, paramObjectID); v != "" {
		req["objectId"] = v
	}
	return presID, []map[string]any{{"createImage": req}}, nil
}

func (p *OverlayProvider) buildCreateTable(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	pageID := strParam(params, paramPageObjectID)
	if presID == "" || pageID == "" {
		return "", nil, fmt.Errorf("%s and %s are required", paramPresentationID, paramPageObjectID)
	}
	rows, rowsOK := intParamOk(params, paramRows)
	cols, colsOK := intParamOk(params, paramColumns)
	if !rowsOK || !colsOK {
		return "", nil, fmt.Errorf("%s and %s are required", paramRows, paramColumns)
	}
	req := map[string]any{
		"rows":    rows,
		"columns": cols,
		"elementProperties": map[string]any{
			"pageObjectId": pageID,
		},
	}
	if v := strParam(params, paramObjectID); v != "" {
		req["objectId"] = v
	}
	return presID, []map[string]any{{"createTable": req}}, nil
}

func (p *OverlayProvider) buildCreateLine(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	pageID := strParam(params, paramPageObjectID)
	if presID == "" || pageID == "" {
		return "", nil, fmt.Errorf("%s and %s are required", paramPresentationID, paramPageObjectID)
	}
	startX := floatParamDefault(params, paramStartX, 100)
	startY := floatParamDefault(params, paramStartY, 100)
	endX := floatParamDefault(params, paramEndX, 400)
	endY := floatParamDefault(params, paramEndY, 100)

	w := math.Abs(endX - startX)
	if w == 0 {
		w = 1
	}
	h := math.Abs(endY - startY)
	if h == 0 {
		h = 1
	}

	req := map[string]any{
		"lineCategory": strParamDefault(params, paramLineCategory, "STRAIGHT"),
		"elementProperties": map[string]any{
			"pageObjectId": pageID,
			"size": map[string]any{
				"width":  dim(w),
				"height": dim(h),
			},
			"transform": map[string]any{
				"scaleX":     1,
				"scaleY":     1,
				"translateX": min(startX, endX),
				"translateY": min(startY, endY),
				"unit":       "PT",
			},
		},
	}
	if v := strParam(params, paramObjectID); v != "" {
		req["objectId"] = v
	}
	return presID, []map[string]any{{"createLine": req}}, nil
}

func (p *OverlayProvider) buildReplaceAllText(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	find := strParam(params, paramFind)
	replacement := strParam(params, paramReplacement)
	if presID == "" || find == "" {
		return "", nil, fmt.Errorf("%s and %s are required", paramPresentationID, paramFind)
	}
	matchCase := boolParamDefault(params, paramMatchCase, true)
	return presID, []map[string]any{{"replaceAllText": map[string]any{
		"containsText": map[string]any{"text": find, "matchCase": matchCase},
		"replaceText":  replacement,
	}}}, nil
}

func (p *OverlayProvider) buildReplaceAllShapesWithImage(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	find := strParam(params, paramFind)
	imageURL := strParam(params, paramImageURL)
	if presID == "" || find == "" || imageURL == "" {
		return "", nil, fmt.Errorf("%s, %s, and %s are required", paramPresentationID, paramFind, paramImageURL)
	}
	return presID, []map[string]any{{"replaceAllShapesWithImage": map[string]any{
		"containsText":       map[string]any{"text": find, "matchCase": true},
		"imageUrl":           imageURL,
		"imageReplaceMethod": strParamDefault(params, paramReplaceMethod, "CENTER_INSIDE"),
	}}}, nil
}

func (p *OverlayProvider) buildUpdateTextStyle(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	objID := strParam(params, paramObjectID)
	if presID == "" || objID == "" {
		return "", nil, fmt.Errorf("%s and %s are required", paramPresentationID, paramObjectID)
	}

	tr := map[string]any{"type": "ALL"}
	if _, hasStart := intParamOk(params, paramStartIndex); hasStart {
		tr = map[string]any{"type": "FIXED_RANGE", "startIndex": mustInt(params, paramStartIndex)}
		if v, ok := intParamOk(params, paramEndIndex); ok {
			tr["endIndex"] = v
		}
	}

	style := map[string]any{}
	fields := []string{}
	addBoolField(params, paramBold, "bold", style, &fields)
	addBoolField(params, paramItalic, "italic", style, &fields)
	addBoolField(params, paramUnderline, "underline", style, &fields)
	if v := strParam(params, paramFontFamily); v != "" {
		style["fontFamily"] = v
		fields = append(fields, "fontFamily")
	}
	if v := floatParamOpt(params, paramFontSize); v > 0 {
		style["fontSize"] = dim(v)
		fields = append(fields, "fontSize")
	}
	if v := strParam(params, paramForegroundColor); v != "" {
		style["foregroundColor"] = hexToRGBA(v)
		fields = append(fields, "foregroundColor")
	}
	if v := strParam(params, paramBackgroundColor); v != "" {
		style["backgroundColor"] = hexToRGBA(v)
		fields = append(fields, "backgroundColor")
	}
	if v := strParam(params, paramLinkURL); v != "" {
		style["link"] = map[string]string{"url": v}
		fields = append(fields, "link")
	}
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("at least one style property must be specified")
	}
	return presID, []map[string]any{{"updateTextStyle": map[string]any{
		"objectId":  objID,
		"textRange": tr,
		"style":     style,
		"fields":    strings.Join(fields, ","),
	}}}, nil
}

func (p *OverlayProvider) buildUpdateShapeProperties(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	objID := strParam(params, paramObjectID)
	if presID == "" || objID == "" {
		return "", nil, fmt.Errorf("%s and %s are required", paramPresentationID, paramObjectID)
	}
	props := map[string]any{}
	fields := []string{}
	if v := strParam(params, paramFillColor); v != "" {
		props["shapeBackgroundFill"] = map[string]any{"solidFill": map[string]any{"color": hexToRGB(v)}}
		fields = append(fields, "shapeBackgroundFill.solidFill.color")
	}
	if v := strParam(params, paramOutlineColor); v != "" {
		outline := ensureMap(props, "outline")
		outline["outlineFill"] = map[string]any{"solidFill": map[string]any{"color": hexToRGB(v)}}
		fields = append(fields, "outline.outlineFill.solidFill.color")
	}
	if v := floatParamOpt(params, paramOutlineWeight); v > 0 {
		outline := ensureMap(props, "outline")
		outline["weight"] = dim(v)
		fields = append(fields, "outline.weight")
	}
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("at least one property must be specified")
	}
	return presID, []map[string]any{{"updateShapeProperties": map[string]any{
		"objectId":        objID,
		"shapeProperties": props,
		"fields":          strings.Join(fields, ","),
	}}}, nil
}

func (p *OverlayProvider) buildUpdatePageProperties(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	objID := strParam(params, paramObjectID)
	if presID == "" || objID == "" {
		return "", nil, fmt.Errorf("%s and %s are required", paramPresentationID, paramObjectID)
	}
	props := map[string]any{}
	fields := []string{}
	if v := strParam(params, paramBackgroundColor); v != "" {
		props["pageBackgroundFill"] = map[string]any{"solidFill": map[string]any{"color": hexToRGB(v)}}
		fields = append(fields, "pageBackgroundFill.solidFill.color")
	}
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("at least one property must be specified")
	}
	return presID, []map[string]any{{"updatePageProperties": map[string]any{
		"objectId":       objID,
		"pageProperties": props,
		"fields":         strings.Join(fields, ","),
	}}}, nil
}

func (p *OverlayProvider) buildUpdateParagraphStyle(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	objID := strParam(params, paramObjectID)
	if presID == "" || objID == "" {
		return "", nil, fmt.Errorf("%s and %s are required", paramPresentationID, paramObjectID)
	}
	style := map[string]any{}
	fields := []string{}
	if v := strParam(params, paramAlignment); v != "" {
		style["alignment"] = v
		fields = append(fields, "alignment")
	}
	if v := floatParamOpt(params, paramLineSpacing); v > 0 {
		style["lineSpacing"] = v
		fields = append(fields, "lineSpacing")
	}
	if v := floatParamOpt(params, paramSpaceAbove); v > 0 {
		style["spaceAbove"] = dim(v)
		fields = append(fields, "spaceAbove")
	}
	if v := floatParamOpt(params, paramSpaceBelow); v > 0 {
		style["spaceBelow"] = dim(v)
		fields = append(fields, "spaceBelow")
	}
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("at least one style property must be specified")
	}
	return presID, []map[string]any{{"updateParagraphStyle": map[string]any{
		"objectId":  objID,
		"textRange": map[string]string{"type": "ALL"},
		"style":     style,
		"fields":    strings.Join(fields, ","),
	}}}, nil
}

func (p *OverlayProvider) buildCreateParagraphBullets(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	objID := strParam(params, paramObjectID)
	if presID == "" || objID == "" {
		return "", nil, fmt.Errorf("%s and %s are required", paramPresentationID, paramObjectID)
	}
	tr := map[string]any{"type": "ALL"}
	if _, hasStart := intParamOk(params, paramStartIndex); hasStart {
		tr = map[string]any{"type": "FIXED_RANGE", "startIndex": mustInt(params, paramStartIndex)}
		if v, ok := intParamOk(params, paramEndIndex); ok {
			tr["endIndex"] = v
		}
	}
	return presID, []map[string]any{{"createParagraphBullets": map[string]any{
		"objectId":     objID,
		"textRange":    tr,
		"bulletPreset": strParamDefault(params, paramBulletPreset, "BULLET_DISC_CIRCLE_SQUARE"),
	}}}, nil
}

func (p *OverlayProvider) buildUpdatePageElementTransform(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	objID := strParam(params, paramObjectID)
	if presID == "" || objID == "" {
		return "", nil, fmt.Errorf("%s and %s are required", paramPresentationID, paramObjectID)
	}
	transform := map[string]any{
		"unit":   "PT",
		"scaleX": floatParamDefault(params, paramScaleX, 1),
		"scaleY": floatParamDefault(params, paramScaleY, 1),
	}
	if _, ok := params[paramTranslateX]; ok {
		transform["translateX"] = floatParamOpt(params, paramTranslateX)
	}
	if _, ok := params[paramTranslateY]; ok {
		transform["translateY"] = floatParamOpt(params, paramTranslateY)
	}
	return presID, []map[string]any{{"updatePageElementTransform": map[string]any{
		"objectId":  objID,
		"transform": transform,
		"applyMode": strParamDefault(params, paramApplyMode, "ABSOLUTE"),
	}}}, nil
}

func (p *OverlayProvider) buildUpdatePageElementsZOrder(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	if presID == "" {
		return "", nil, fmt.Errorf("%s is required", paramPresentationID)
	}
	ids := strSliceParam(params, paramPageElementObjectIDs)
	op := strParam(params, paramOperation)
	if len(ids) == 0 || op == "" {
		return "", nil, fmt.Errorf("%s and %s are required", paramPageElementObjectIDs, paramOperation)
	}
	return presID, []map[string]any{{"updatePageElementsZOrder": map[string]any{
		"pageElementObjectIds": ids,
		"operation":            op,
	}}}, nil
}

func (p *OverlayProvider) buildGroupObjects(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	if presID == "" {
		return "", nil, fmt.Errorf("%s is required", paramPresentationID)
	}
	ids := strSliceParam(params, paramChildrenObjectIDs)
	if len(ids) < 2 {
		return "", nil, fmt.Errorf("%s requires at least 2 element IDs", paramChildrenObjectIDs)
	}
	req := map[string]any{"childrenObjectIds": ids}
	if v := strParam(params, paramGroupObjectID); v != "" {
		req["groupObjectId"] = v
	}
	return presID, []map[string]any{{"groupObjects": req}}, nil
}

func (p *OverlayProvider) buildUngroupObjects(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	if presID == "" {
		return "", nil, fmt.Errorf("%s is required", paramPresentationID)
	}
	ids := strSliceParam(params, paramObjectIDs)
	if len(ids) == 0 {
		return "", nil, fmt.Errorf("%s is required", paramObjectIDs)
	}
	return presID, []map[string]any{{"ungroupObjects": map[string]any{"objectIds": ids}}}, nil
}

func (p *OverlayProvider) buildUpdateTableCellProperties(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	objID := strParam(params, paramObjectID)
	if presID == "" || objID == "" {
		return "", nil, fmt.Errorf("%s and %s are required", paramPresentationID, paramObjectID)
	}
	rowIdx, rowOK := intParamOk(params, paramRowIndex)
	colIdx, colOK := intParamOk(params, paramColumnIndex)
	if !rowOK || !colOK {
		return "", nil, fmt.Errorf("%s and %s are required", paramRowIndex, paramColumnIndex)
	}
	props := map[string]any{}
	fields := []string{}
	if v := strParam(params, paramBackgroundColor); v != "" {
		props["tableCellBackgroundFill"] = map[string]any{"solidFill": map[string]any{"color": hexToRGB(v)}}
		fields = append(fields, "tableCellBackgroundFill.solidFill.color")
	}
	for _, pair := range []struct{ param, api string }{
		{paramPaddingTop, "paddingTop"},
		{paramPaddingBottom, "paddingBottom"},
		{paramPaddingLeft, "paddingLeft"},
		{paramPaddingRight, "paddingRight"},
	} {
		if v := floatParamOpt(params, pair.param); v > 0 {
			props[pair.api] = dim(v)
			fields = append(fields, pair.api)
		}
	}
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("at least one property must be specified")
	}
	return presID, []map[string]any{{"updateTableCellProperties": map[string]any{
		"objectId": objID,
		"tableRange": map[string]any{
			"location":   map[string]any{"rowIndex": rowIdx, "columnIndex": colIdx},
			"rowSpan":    1,
			"columnSpan": 1,
		},
		"tableCellProperties": props,
		"fields":              strings.Join(fields, ","),
	}}}, nil
}

func (p *OverlayProvider) buildCreateStyledTextBox(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	pageID := strParam(params, paramPageObjectID)
	text := strParam(params, paramText)
	if presID == "" || pageID == "" || text == "" {
		return "", nil, fmt.Errorf("%s, %s, and %s are required", paramPresentationID, paramPageObjectID, paramText)
	}
	objID := strParam(params, paramObjectID)
	if objID == "" {
		objID = uniqueID("styledtextbox_")
	}

	reqs := []map[string]any{
		{"createShape": map[string]any{
			"objectId":          objID,
			"shapeType":         "TEXT_BOX",
			"elementProperties": elementProperties(pageID, params, 100, 100, 300, 100),
		}},
		{"insertText": map[string]any{"objectId": objID, "text": text, "insertionIndex": 0}},
	}

	style := map[string]any{}
	fields := []string{}
	if v := strParam(params, paramFontFamily); v != "" {
		style["fontFamily"] = v
		fields = append(fields, "fontFamily")
	}
	if v := floatParamOpt(params, paramFontSize); v > 0 {
		style["fontSize"] = dim(v)
		fields = append(fields, "fontSize")
	}
	addBoolField(params, paramBold, "bold", style, &fields)
	addBoolField(params, paramItalic, "italic", style, &fields)
	if v := strParam(params, paramForegroundColor); v != "" {
		style["foregroundColor"] = hexToRGBA(v)
		fields = append(fields, "foregroundColor")
	}
	if len(fields) > 0 {
		reqs = append(reqs, map[string]any{"updateTextStyle": map[string]any{
			"objectId":  objID,
			"textRange": map[string]string{"type": "ALL"},
			"style":     style,
			"fields":    strings.Join(fields, ","),
		}})
	}

	if v := strParam(params, paramFillColor); v != "" {
		reqs = append(reqs, map[string]any{"updateShapeProperties": map[string]any{
			"objectId": objID,
			"shapeProperties": map[string]any{
				"shapeBackgroundFill": map[string]any{"solidFill": map[string]any{"color": hexToRGB(v)}},
			},
			"fields": "shapeBackgroundFill.solidFill.color",
		}})
	}

	if v := strParam(params, paramAlignment); v != "" {
		reqs = append(reqs, map[string]any{"updateParagraphStyle": map[string]any{
			"objectId":  objID,
			"textRange": map[string]string{"type": "ALL"},
			"style":     map[string]string{"alignment": v},
			"fields":    "alignment",
		}})
	}

	return presID, reqs, nil
}

func (p *OverlayProvider) buildCreateBulletList(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	pageID := strParam(params, paramPageObjectID)
	if presID == "" || pageID == "" {
		return "", nil, fmt.Errorf("%s and %s are required", paramPresentationID, paramPageObjectID)
	}
	items := strSliceParam(params, paramItems)
	if len(items) == 0 {
		return "", nil, fmt.Errorf("%s is required and must be a list", paramItems)
	}
	objID := strParam(params, paramObjectID)
	if objID == "" {
		objID = uniqueID("bulletlist_")
	}
	text := strings.Join(items, "\n")

	reqs := []map[string]any{
		{"createShape": map[string]any{
			"objectId":          objID,
			"shapeType":         "TEXT_BOX",
			"elementProperties": elementProperties(pageID, params, 50, 100, 400, 250),
		}},
		{"insertText": map[string]any{"objectId": objID, "text": text, "insertionIndex": 0}},
		{"createParagraphBullets": map[string]any{
			"objectId":     objID,
			"textRange":    map[string]string{"type": "ALL"},
			"bulletPreset": strParamDefault(params, paramBulletPreset, "BULLET_DISC_CIRCLE_SQUARE"),
		}},
	}

	style := map[string]any{}
	fields := []string{}
	if v := strParam(params, paramFontFamily); v != "" {
		style["fontFamily"] = v
		fields = append(fields, "fontFamily")
	}
	if v := floatParamOpt(params, paramFontSize); v > 0 {
		style["fontSize"] = dim(v)
		fields = append(fields, "fontSize")
	}
	if v := strParam(params, paramForegroundColor); v != "" {
		style["foregroundColor"] = hexToRGBA(v)
		fields = append(fields, "foregroundColor")
	}
	if len(fields) > 0 {
		reqs = append(reqs, map[string]any{"updateTextStyle": map[string]any{
			"objectId":  objID,
			"textRange": map[string]string{"type": "ALL"},
			"style":     style,
			"fields":    strings.Join(fields, ","),
		}})
	}

	return presID, reqs, nil
}

func (p *OverlayProvider) buildCreateTitleSlide(params map[string]any) (string, []map[string]any, error) {
	presID := strParam(params, paramPresentationID)
	title := strParam(params, paramTitle)
	if presID == "" || title == "" {
		return "", nil, fmt.Errorf("%s and %s are required", paramPresentationID, paramTitle)
	}

	slideID := uniqueID("titleslide_")
	titleBoxID := uniqueID("titlebox_")
	subtitleBoxID := uniqueID("subtitlebox_")

	reqs := []map[string]any{}
	createSlide := map[string]any{
		"objectId":             slideID,
		"slideLayoutReference": map[string]string{"predefinedLayout": "BLANK"},
	}
	if v, ok := intParamOk(params, paramInsertionIndex); ok {
		createSlide["insertionIndex"] = v
	}
	reqs = append(reqs, map[string]any{"createSlide": createSlide})

	if v := strParam(params, paramBackgroundColor); v != "" {
		reqs = append(reqs, map[string]any{"updatePageProperties": map[string]any{
			"objectId":       slideID,
			"pageProperties": map[string]any{"pageBackgroundFill": map[string]any{"solidFill": map[string]any{"color": hexToRGB(v)}}},
			"fields":         "pageBackgroundFill.solidFill.color",
		}})
	}

	reqs = append(reqs,
		map[string]any{"createShape": map[string]any{
			"objectId":  titleBoxID,
			"shapeType": "TEXT_BOX",
			"elementProperties": map[string]any{
				"pageObjectId": slideID,
				"size":         map[string]any{"width": dim(620), "height": dim(80)},
				"transform":    map[string]any{"scaleX": 1, "scaleY": 1, "translateX": 50, "translateY": 120, "unit": "PT"},
			},
		}},
		map[string]any{"insertText": map[string]any{"objectId": titleBoxID, "text": title, "insertionIndex": 0}},
	)

	titleColor := strParamDefault(params, paramTitleColor, "#000000")
	reqs = append(reqs,
		map[string]any{"updateTextStyle": map[string]any{
			"objectId":  titleBoxID,
			"textRange": map[string]string{"type": "ALL"},
			"style": map[string]any{
				"fontFamily":      strParamDefault(params, paramTitleFont, "Arial"),
				"fontSize":        dim(floatParamDefault(params, paramTitleSize, 44)),
				"bold":            true,
				"foregroundColor": hexToRGBA(titleColor),
			},
			"fields": "fontFamily,fontSize,bold,foregroundColor",
		}},
		map[string]any{"updateParagraphStyle": map[string]any{
			"objectId":  titleBoxID,
			"textRange": map[string]string{"type": "ALL"},
			"style":     map[string]string{"alignment": "CENTER"},
			"fields":    "alignment",
		}},
	)

	if subtitle := strParam(params, paramSubtitle); subtitle != "" {
		reqs = append(reqs,
			map[string]any{"createShape": map[string]any{
				"objectId":  subtitleBoxID,
				"shapeType": "TEXT_BOX",
				"elementProperties": map[string]any{
					"pageObjectId": slideID,
					"size":         map[string]any{"width": dim(620), "height": dim(50)},
					"transform":    map[string]any{"scaleX": 1, "scaleY": 1, "translateX": 50, "translateY": 210, "unit": "PT"},
				},
			}},
			map[string]any{"insertText": map[string]any{"objectId": subtitleBoxID, "text": subtitle, "insertionIndex": 0}},
		)

		subColor := strParamDefault(params, paramSubtitleColor, titleColor)
		reqs = append(reqs,
			map[string]any{"updateTextStyle": map[string]any{
				"objectId":  subtitleBoxID,
				"textRange": map[string]string{"type": "ALL"},
				"style": map[string]any{
					"fontFamily":      strParamDefault(params, paramSubtitleFont, "Arial"),
					"fontSize":        dim(floatParamDefault(params, paramSubtitleSize, 24)),
					"foregroundColor": hexToRGBA(subColor),
				},
				"fields": "fontFamily,fontSize,foregroundColor",
			}},
			map[string]any{"updateParagraphStyle": map[string]any{
				"objectId":  subtitleBoxID,
				"textRange": map[string]string{"type": "ALL"},
				"style":     map[string]string{"alignment": "CENTER"},
				"fields":    "alignment",
			}},
		)
	}

	return presID, reqs, nil
}

func (p *OverlayProvider) batchUpdate(ctx context.Context, presentationID string, requests []map[string]any, token string) (*core.OperationResult, error) {
	url := fmt.Sprintf("%s/presentations/%s:batchUpdate", slidesAPIBase, presentationID)
	payload, _ := json.Marshal(map[string]any{"requests": requests})
	return p.doPost(ctx, url, payload, token)
}

func (p *OverlayProvider) doPost(ctx context.Context, url string, payload []byte, token string) (*core.OperationResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentTypeJSON)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &core.OperationResult{Status: resp.StatusCode, Body: string(data)}, nil
}

func uniqueID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

func hexToRGBA(hex string) map[string]any {
	return map[string]any{"opaqueColor": map[string]any{"rgbColor": parseRGB(hex)}}
}

func hexToRGB(hex string) map[string]any {
	return map[string]any{"rgbColor": parseRGB(hex)}
}

func parseRGB(hex string) map[string]any {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) < 6 {
		hex = "000000"
	}
	r, _ := strconv.ParseUint(hex[0:2], 16, 8)
	g, _ := strconv.ParseUint(hex[2:4], 16, 8)
	b, _ := strconv.ParseUint(hex[4:6], 16, 8)
	return map[string]any{"red": float64(r) / 255, "green": float64(g) / 255, "blue": float64(b) / 255}
}

func dim(magnitude float64) map[string]any {
	return map[string]any{"magnitude": magnitude, "unit": "PT"}
}

func elementProperties(pageObjectID string, params map[string]any, defX, defY, defW, defH float64) map[string]any {
	x := floatParamDefault(params, paramX, defX)
	y := floatParamDefault(params, paramY, defY)
	w := floatParamDefault(params, paramWidth, defW)
	h := floatParamDefault(params, paramHeight, defH)
	return map[string]any{
		"pageObjectId": pageObjectID,
		"size": map[string]any{
			"width":  dim(w),
			"height": dim(h),
		},
		"transform": map[string]any{
			"scaleX":     1,
			"scaleY":     1,
			"translateX": x,
			"translateY": y,
			"unit":       "PT",
		},
	}
}

func ensureMap(parent map[string]any, key string) map[string]any {
	if m, ok := parent[key].(map[string]any); ok {
		return m
	}
	m := map[string]any{}
	parent[key] = m
	return m
}

func addBoolField(params map[string]any, paramName, fieldName string, style map[string]any, fields *[]string) {
	if v, ok := params[paramName]; ok {
		if b, ok := v.(bool); ok {
			style[fieldName] = b
			*fields = append(*fields, fieldName)
		}
	}
}

func strParam(params map[string]any, key string) string {
	v, _ := params[key].(string)
	return v
}

func strParamDefault(params map[string]any, key, def string) string {
	if v := strParam(params, key); v != "" {
		return v
	}
	return def
}

func intParamOk(params map[string]any, key string) (int, bool) {
	switch v := params[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	}
	return 0, false
}

func mustInt(params map[string]any, key string) int {
	v, _ := intParamOk(params, key)
	return v
}

func floatParamOpt(params map[string]any, key string) float64 {
	switch v := params[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	}
	return 0
}

func floatParamDefault(params map[string]any, key string, def float64) float64 {
	if _, ok := params[key]; ok {
		return floatParamOpt(params, key)
	}
	return def
}

func boolParamDefault(params map[string]any, key string, def bool) bool {
	if v, ok := params[key].(bool); ok {
		return v
	}
	return def
}

func strSliceParam(params map[string]any, key string) []string {
	switch v := params[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

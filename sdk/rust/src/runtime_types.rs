#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct RuntimeMetadata {
    pub name: String,
    pub display_name: String,
    pub description: String,
    pub version: String,
}

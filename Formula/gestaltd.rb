# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.55"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.55/gestaltd-macos-arm64.tar.gz"
      sha256 "f5a0de87ddbdbb831e878350b5893a3e19cbd9f866bb5e61b6679fbb6193733e"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.55/gestaltd-macos-x86_64.tar.gz"
      sha256 "040bc5dd6546132c085c7eaba3c99fcc71d50bb02c13374fcb4f7a5c9e89222a"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.55/gestaltd-linux-arm64.tar.gz"
      sha256 "c7c2077e9ae90578409d9dd405d63826a8a08d313d950f2730c45665bf0b8437"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.55/gestaltd-linux-x86_64.tar.gz"
      sha256 "6939eb2eb90194901e189a28853844802fc10878d67d217a38906ad3ed5596e4"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

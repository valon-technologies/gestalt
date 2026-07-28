# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.48"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.48/gestaltd-macos-arm64.tar.gz"
      sha256 "fc41f5d686ff44f91dc84603007f101b772e32a54f5544975e2d2dc6d3c06675"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.48/gestaltd-macos-x86_64.tar.gz"
      sha256 "d24f85d1e82c616e82ed44cca298344187a4aa9f8e367e33694681747c7a3a1b"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.48/gestaltd-linux-arm64.tar.gz"
      sha256 "12d6cbe2718e639604ce538b96b4a50503f1908ae16b15d69587d072c3dfc83d"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.48/gestaltd-linux-x86_64.tar.gz"
      sha256 "649752d7929ce54f3d1ad4eed779c65a89e7a1bee98c0f264a54d8e39c4a7697"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

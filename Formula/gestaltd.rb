# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1/gestaltd-macos-arm64.tar.gz"
      sha256 "7b149f0dd8405e29393ed8be5c9fef4452295cf0bfd1f495545e501b6fb60aaa"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1/gestaltd-macos-x86_64.tar.gz"
      sha256 "ef246fb34b5282c0b9e0b01ee8ca4df8d3e9a1df6b8ac2215ea0ef1aacff77df"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1/gestaltd-linux-arm64.tar.gz"
      sha256 "2df7fef45f30cce3232747a55e4c9d26a6c77ef76caa13abf77636ec8000e6d8"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1/gestaltd-linux-x86_64.tar.gz"
      sha256 "070a465feffb453341a7949464e9420ab48939456238406560f5240433de5cd3"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

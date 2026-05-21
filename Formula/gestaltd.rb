# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.3"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.3/gestaltd-macos-arm64.tar.gz"
      sha256 "51384a343740c83d8d02637cd08b47f1cf9135fb3b1716ec5a0045917047b184"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.3/gestaltd-macos-x86_64.tar.gz"
      sha256 "e4cca57a1ae432760651f46896ebedf5ba34e044b414e876c5a00df5f4919d8d"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.3/gestaltd-linux-arm64.tar.gz"
      sha256 "9add99faa74e0beea2ec8a32bd183205ef0f84df5a588e4d5f8340995259fc65"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.3/gestaltd-linux-x86_64.tar.gz"
      sha256 "cc6afcffcf70f2852fdfeccd11e2122d76f040aea7a494466cb3ad4ef10977ef"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end

# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.28"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.28/gestaltd-macos-arm64.tar.gz"
      sha256 "fdad7eadac0f2f3f0982487313d8d0f254c72c3e68b4c1e081c211156d7f6099"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.28/gestaltd-macos-x86_64.tar.gz"
      sha256 "61caa947e3d3e346b59af3efdeed99cea54d801709abe9e9c26f724cc6a27d10"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.28/gestaltd-linux-arm64.tar.gz"
      sha256 "5687a47e674ac0227beb7bc4fb18297267bb4f95e2ebfb9f06d4d2792cffc829"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.28/gestaltd-linux-x86_64.tar.gz"
      sha256 "0d339febde8bdb4f1fce0ad3c9d74b2a841789676d22bad02be021487f1354ac"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
